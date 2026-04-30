#include <dirent.h>
#include <errno.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include <curl/curl.h>
#include <openssl/sha.h>

#include <libyang/libyang.h>
#include <nc_server.h>

static volatile sig_atomic_t g_stop = 0;
static const char *g_snapshot = NULL;
static const char *g_running = NULL;
static const char *g_candidate = NULL;
static const char *g_control = NULL;
static const char *g_control_unix = NULL;

#define MAX_TRACKED_SESSIONS 64
#define NOTIFICATION_POLL_MS 250

struct session_state {
  struct nc_session *session;
  int subscribed;
};

static struct session_state g_sessions[MAX_TRACKED_SESSIONS];

struct http_buf {
  char *ptr;
  size_t len;
};

static void on_sigint(int sig) {
  (void)sig;
  g_stop = 1;
}

static void sha256_hex(const char *data, char out[65]) {
  unsigned char hash[SHA256_DIGEST_LENGTH];
  SHA256((const unsigned char *)data, strlen(data), hash);
  for (int i = 0; i < SHA256_DIGEST_LENGTH; ++i) {
    sprintf(out + (i * 2), "%02x", hash[i]);
  }
  out[64] = '\0';
}

static void emit_netconf_get(const struct nc_session *session,
                             const char *raw) {
  char ts[32] = {0};
  time_t now = time(NULL);
  struct tm tm_now;
  localtime_r(&now, &tm_now);
  strftime(ts, sizeof(ts), "%Y-%m-%dT%H:%M:%S%z", &tm_now);
  const char *user = nc_session_get_username(session);

  if (!raw) {
    raw = "";
  }
  size_t bytes = strlen(raw);
  char sha[65];
  sha256_hex(raw, sha);

  fprintf(stdout, "NETCONF_GET user=%s ts=%s bytes=%zu sha256=%s",
          user ? user : "unknown", ts, bytes, sha);
  if (bytes <= 16384) {
    fprintf(stdout, " json=%s", raw);
  }
  fprintf(stdout, "\n");
  fflush(stdout);
}

static char *read_file(const char *path) {
  FILE *f;
  long size;
  size_t n;
  char *buf;

  if (!path || !path[0]) {
    return NULL;
  }
  f = fopen(path, "rb");
  if (!f) {
    return NULL;
  }
  if (fseek(f, 0, SEEK_END) != 0) {
    fclose(f);
    return NULL;
  }
  size = ftell(f);
  if (size < 0) {
    fclose(f);
    return NULL;
  }
  rewind(f);

  buf = (char *)malloc((size_t)size + 1);
  if (!buf) {
    fclose(f);
    return NULL;
  }
  n = fread(buf, 1, (size_t)size, f);
  fclose(f);
  buf[n] = '\0';
  return buf;
}

static struct lyd_node *load_json_tree(const struct ly_ctx *ctx,
                                       const char *raw) {
  struct lyd_node *tree = NULL;
  if (!ctx || !raw || !raw[0]) {
    return NULL;
  }
  if (lyd_parse_data_mem(ctx, raw, LYD_JSON, LYD_PARSE_STRICT,
                         LYD_VALIDATE_PRESENT, &tree)) {
    lyd_free_all(tree);
    return NULL;
  }
  return tree;
}

static struct lyd_node *load_json_file_tree(const struct ly_ctx *ctx,
                                            const char *path) {
  char *raw = read_file(path);
  struct lyd_node *tree = load_json_tree(ctx, raw);
  free(raw);
  return tree;
}

static const char *meta_value_by_name(const struct lyd_node *n,
                                      const char *name) {
  struct lyd_meta *m;
  LY_LIST_FOR(n->meta, m) {
    if (!strcmp(m->name, name)) {
      return lyd_get_meta_value(m);
    }
  }
  return NULL;
}

static const struct lyd_node *child_by_name_mod(const struct lyd_node *parent,
                                                const char *name,
                                                const char *module) {
  const struct lyd_node *ch;
  LY_LIST_FOR(lyd_child(parent), ch) {
    if (strcmp(LYD_NAME(ch), name)) {
      continue;
    }
    if (module && strcmp(lyd_owner_module(ch)->name, module)) {
      continue;
    }
    return ch;
  }
  return NULL;
}

static const struct lyd_node *child_by_name(const struct lyd_node *parent,
                                            const char *name) {
  return child_by_name_mod(parent, name, NULL);
}

static const struct lyd_node *find_descendant(const struct lyd_node *root,
                                              const char *name) {
  const struct lyd_node *it;
  if (!root) {
    return NULL;
  }
  LY_LIST_FOR(root, it) {
    if (!strcmp(LYD_NAME(it), name)) {
      return it;
    }
    const struct lyd_node *found = find_descendant(lyd_child(it), name);
    if (found) {
      return found;
    }
  }
  return NULL;
}

static size_t curl_write_cb(char *contents, size_t size, size_t nmemb,
                            void *userdata) {
  size_t n = size * nmemb;
  struct http_buf *b = (struct http_buf *)userdata;
  char *p = (char *)realloc(b->ptr, b->len + n + 1);
  if (!p) {
    return 0;
  }
  b->ptr = p;
  memcpy(b->ptr + b->len, contents, n);
  b->len += n;
  b->ptr[b->len] = '\0';
  return n;
}

static void curl_apply_control_unix(CURL *curl) {
  if (g_control_unix && g_control_unix[0]) {
    curl_easy_setopt(curl, CURLOPT_UNIX_SOCKET_PATH, g_control_unix);
  }
}

static int http_post_raw(const char *url, const char *payload,
                         struct curl_slist *headers, long timeout_sec,
                         long *status, char **response) {
  CURL *curl = curl_easy_init();
  struct http_buf b = {.ptr = NULL, .len = 0};
  CURLcode rc;
  long code = 0;

  if (!curl) {
    return -1;
  }
  curl_easy_setopt(curl, CURLOPT_URL, url);
  curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
  curl_easy_setopt(curl, CURLOPT_POST, 1L);
  curl_easy_setopt(curl, CURLOPT_POSTFIELDS, payload ? payload : "");
  curl_easy_setopt(curl, CURLOPT_POSTFIELDSIZE,
                   payload ? (long)strlen(payload) : 0L);
  curl_easy_setopt(curl, CURLOPT_TIMEOUT, timeout_sec);
  curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 2L);
  curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curl_write_cb);
  curl_easy_setopt(curl, CURLOPT_WRITEDATA, &b);
  curl_apply_control_unix(curl);

  rc = curl_easy_perform(curl);
  if (rc != CURLE_OK) {
    curl_easy_cleanup(curl);
    free(b.ptr);
    return -1;
  }
  curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &code);
  curl_easy_cleanup(curl);

  if (status) {
    *status = code;
  }
  if (response) {
    *response = b.ptr;
    b.ptr = NULL;
  }
  free(b.ptr);
  return 0;
}

static char *extract_message(const char *json) {
  const char *p;
  const char *q;
  char *out;
  size_t len;

  if (!json) {
    return NULL;
  }
  p = strstr(json, "\"message\"");
  if (!p) {
    return NULL;
  }
  p = strchr(p, ':');
  if (!p) {
    return NULL;
  }
  ++p;
  while ((*p == ' ') || (*p == '\t')) {
    ++p;
  }
  if (*p != '"') {
    return NULL;
  }
  ++p;
  q = p;
  while (*q && (*q != '"')) {
    if ((*q == '\\') && *(q + 1)) {
      q += 2;
      continue;
    }
    ++q;
  }
  if (*q != '"') {
    return NULL;
  }
  len = (size_t)(q - p);
  out = (char *)malloc(len + 1);
  if (!out) {
    return NULL;
  }
  memcpy(out, p, len);
  out[len] = '\0';
  return out;
}

static struct nc_server_reply *rpc_error_msg(const struct ly_ctx *ctx,
                                             NC_ERR tag, const char *msg) {
  struct lyd_node *err = nc_err(ctx, tag, NC_ERR_TYPE_APP);
  if (err && msg && msg[0]) {
    nc_err_set_msg(err, msg, "en");
  }
  return nc_server_reply_err(err);
}

static struct curl_slist *add_common_headers(struct curl_slist *headers,
                                             const struct nc_session *session) {
  char buf[128];
  const char *user = nc_session_get_username(session);

  snprintf(buf, sizeof(buf), "X-NETCONF-Session-ID: %u",
           nc_session_get_id(session));
  headers = curl_slist_append(headers, buf);
  if (user && user[0]) {
    char user_hdr[512];
    snprintf(user_hdr, sizeof(user_hdr), "X-NETCONF-Username: %s", user);
    headers = curl_slist_append(headers, user_hdr);
  }
  return headers;
}

static int control_post(const struct nc_session *session, const char *path,
                        const char *payload, long timeout_sec,
                        struct curl_slist *headers, long *status,
                        char **response) {
  char url[512];
  int rc;
  if (!g_control || !g_control[0]) {
    return -1;
  }
  snprintf(url, sizeof(url), "%s%s", g_control, path);
  headers = add_common_headers(headers, session);
  rc = http_post_raw(url, payload, headers, timeout_sec, status, response);
  curl_slist_free_all(headers);
  return rc;
}

static int control_post_system(const char *path, const char *payload,
                               long timeout_sec, long *status,
                               char **response) {
  char url[512];
  if (!g_control || !g_control[0]) {
    return -1;
  }
  snprintf(url, sizeof(url), "%s%s", g_control, path);
  return http_post_raw(url, payload, NULL, timeout_sec, status, response);
}

static struct session_state *find_session_state(struct nc_session *session) {
  if (!session) {
    return NULL;
  }
  for (size_t i = 0; i < MAX_TRACKED_SESSIONS; ++i) {
    if (g_sessions[i].session == session) {
      return &g_sessions[i];
    }
  }
  return NULL;
}

static void track_session(struct nc_session *session) {
  if (!session || find_session_state(session)) {
    return;
  }
  for (size_t i = 0; i < MAX_TRACKED_SESSIONS; ++i) {
    if (!g_sessions[i].session) {
      g_sessions[i].session = session;
      g_sessions[i].subscribed = 0;
      return;
    }
  }
}

static void untrack_session(struct nc_session *session) {
  struct session_state *st = find_session_state(session);
  if (!st) {
    return;
  }
  if (st->subscribed && nc_session_get_notif_status(session) > 0) {
    nc_session_dec_notif_status(session);
  }
  st->session = NULL;
  st->subscribed = 0;
}

static int any_subscribed_session(void) {
  for (size_t i = 0; i < MAX_TRACKED_SESSIONS; ++i) {
    if (g_sessions[i].session && g_sessions[i].subscribed &&
        nc_session_get_notif_status(g_sessions[i].session) > 0) {
      return 1;
    }
  }
  return 0;
}

static int load_yang_dir_modules(struct ly_ctx *ctx, const char *dirs) {
  char *copy;
  char *dir;
  if (!ctx || !dirs || !dirs[0]) {
    return -1;
  }
  copy = strdup(dirs);
  if (!copy) {
    return -1;
  }
  for (dir = strtok(copy, ":"); dir; dir = strtok(NULL, ":")) {
    DIR *dh = opendir(dir);
    struct dirent *de;
    if (ly_ctx_set_searchdir(ctx, dir)) {
      free(copy);
      return -1;
    }
    if (!dh) {
      continue;
    }
    while ((de = readdir(dh)) != NULL) {
      size_t len = strlen(de->d_name);
      char path[4096];
      if ((len < 5) || strcmp(de->d_name + len - 5, ".yang")) {
        continue;
      }
      snprintf(path, sizeof(path), "%s/%s", dir, de->d_name);
      if (lys_parse_path(ctx, path, LYS_IN_YANG, NULL)) {
        closedir(dh);
        free(copy);
        return -1;
      }
    }
    closedir(dh);
  }
  free(copy);
  return 0;
}

static struct lyd_node *filter_by_xpath(struct lyd_node *root,
                                        const char *xpath) {
  struct ly_set *set = NULL;
  struct lyd_node *out = NULL;
  uint32_t i;

  if (!root || !xpath || !xpath[0]) {
    return root;
  }
  if (lyd_find_xpath(root, xpath, &set)) {
    ly_set_free(set, NULL);
    return NULL;
  }
  for (i = 0; set && (i < set->count); ++i) {
    struct lyd_node *dup = NULL;
    if (lyd_dup_single(set->dnodes[i], NULL,
                       LYD_DUP_RECURSIVE | LYD_DUP_WITH_PARENTS, &dup)) {
      lyd_free_all(out);
      ly_set_free(set, NULL);
      return NULL;
    }
    while (dup->parent) {
      dup = lyd_parent(dup);
    }
    if (lyd_merge_tree(&out, dup, LYD_MERGE_DESTRUCT)) {
      lyd_free_all(dup);
      lyd_free_all(out);
      ly_set_free(set, NULL);
      return NULL;
    }
  }
  ly_set_free(set, NULL);
  return out;
}

static int nodes_same_name(const struct lyd_node *a, const struct lyd_node *b) {
  return a && b && !strcmp(LYD_NAME(a), LYD_NAME(b)) &&
         !strcmp(lyd_owner_module(a)->name, lyd_owner_module(b)->name);
}

static int node_matches_filter(const struct lyd_node *data,
                               const struct lyd_node *filter) {
  const struct lyd_node *fch;
  if (!nodes_same_name(data, filter)) {
    return 0;
  }
  LY_LIST_FOR(lyd_child(filter), fch) {
    const struct lyd_node *dch = NULL;
    int matched = 0;
    LY_LIST_FOR(lyd_child(data), dch) {
      if (!nodes_same_name(dch, fch)) {
        continue;
      }
      if (fch->schema && ((fch->schema->nodetype == LYS_LEAF) ||
                          (fch->schema->nodetype == LYS_LEAFLIST))) {
        const char *fv = lyd_get_value(fch);
        const char *dv = lyd_get_value(dch);
        if (!fv || !fv[0] || !strcmp(fv, dv)) {
          matched = 1;
          break;
        }
      } else if (node_matches_filter(dch, fch)) {
        matched = 1;
        break;
      }
    }
    if (!matched) {
      return 0;
    }
  }
  return 1;
}

static int collect_subtree_matches(const struct lyd_node *data,
                                   const struct lyd_node *filter,
                                   struct lyd_node **out) {
  const struct lyd_node *it;
  LY_LIST_FOR(data, it) {
    if (node_matches_filter(it, filter)) {
      struct lyd_node *dup = NULL;
      if (lyd_dup_single(it, NULL, LYD_DUP_RECURSIVE | LYD_DUP_WITH_PARENTS,
                         &dup)) {
        return -1;
      }
      while (dup->parent) {
        dup = lyd_parent(dup);
      }
      if (lyd_merge_tree(out, dup, LYD_MERGE_DESTRUCT)) {
        lyd_free_all(dup);
        return -1;
      }
    }
    if (collect_subtree_matches(lyd_child(it), filter, out)) {
      return -1;
    }
  }
  return 0;
}

static struct lyd_node *filter_by_subtree(struct lyd_node *data,
                                          struct lyd_node *filter_tree) {
  struct lyd_node *out = NULL;
  const struct lyd_node *it;
  if (!data || !filter_tree) {
    return NULL;
  }
  LY_LIST_FOR(filter_tree, it) {
    if (collect_subtree_matches(data, it, &out)) {
      lyd_free_all(out);
      return NULL;
    }
  }
  return out;
}

static struct lyd_node *
apply_filter_to_tree(const struct ly_ctx *ctx, struct lyd_node *data,
                     const struct lyd_node *rpc_filter) {
  const char *type;
  char *xml = NULL;
  struct lyd_node *filter_tree = NULL;
  struct lyd_node *out = NULL;

  if (!data || !rpc_filter) {
    return data;
  }
  type = meta_value_by_name(rpc_filter, "type");
  if (type && !strcmp(type, "xpath")) {
    const char *xpath = meta_value_by_name(rpc_filter, "select");
    return filter_by_xpath(data, xpath);
  }
  if (lyd_any_value_str(rpc_filter, &xml) || !xml || !xml[0]) {
    free(xml);
    return NULL;
  }
  if (lyd_parse_data_mem(ctx, xml, LYD_XML, LYD_PARSE_ONLY, 0, &filter_tree)) {
    free(xml);
    lyd_free_all(filter_tree);
    return NULL;
  }
  free(xml);
  out = filter_by_subtree(data, filter_tree);
  lyd_free_all(filter_tree);
  return out;
}

static struct nc_server_reply *reply_with_tree(struct lyd_node *rpc,
                                               struct nc_session *session,
                                               struct lyd_node *tree) {
  struct lyd_node *reply = NULL;
  struct nc_server_reply *rpl;

  if (tree) {
    char *json = NULL;
    if (!lyd_print_mem(&json, tree, LYD_JSON, LYD_PRINT_SHRINK)) {
      emit_netconf_get(session, json);
    }
    free(json);
  }

  if (lyd_dup_single(rpc, NULL, 0, &reply)) {
    lyd_free_all(tree);
    return nc_server_reply_ok();
  }
  if (tree) {
    if (lyd_new_any(reply, NULL, "data", tree, LYD_ANYDATA_DATATREE,
                    LYD_NEW_ANY_USE_VALUE | LYD_NEW_VAL_OUTPUT, NULL)) {
      lyd_free_all(reply);
      lyd_free_all(tree);
      return nc_server_reply_ok();
    }
  } else {
    if (lyd_new_any(reply, NULL, "data", "", LYD_ANYDATA_STRING,
                    LYD_NEW_VAL_OUTPUT, NULL)) {
      lyd_free_all(reply);
      return nc_server_reply_ok();
    }
  }
  rpl = nc_server_reply_data(reply, NC_WD_UNKNOWN, NC_PARAMTYPE_FREE);
  if (!rpl) {
    return nc_server_reply_ok();
  }
  return rpl;
}

static struct lyd_node *load_full_get_tree(const struct ly_ctx *ctx) {
  struct lyd_node *snap = load_json_file_tree(ctx, g_snapshot);
  struct lyd_node *running = load_json_file_tree(ctx, g_running);
  if (snap && running) {
    if (lyd_merge_siblings(&snap, running, LYD_MERGE_DESTRUCT)) {
      lyd_free_all(snap);
      lyd_free_all(running);
      return NULL;
    }
  } else if (!snap) {
    snap = running;
    running = NULL;
  }
  return snap;
}

static struct nc_server_reply *handle_get_rpc(struct lyd_node *rpc,
                                              struct nc_session *session) {
  const struct ly_ctx *ctx = nc_session_get_ctx(session);
  struct lyd_node *data = load_full_get_tree(ctx);
  struct lyd_node *out = data;
  struct lyd_node *rpc_filter = NULL;
  LY_ERR ret;

  ret = lyd_find_path(rpc, "filter", 0, &rpc_filter);
  if (ret && (ret != LY_ENOTFOUND)) {
    lyd_free_all(data);
    return reply_with_tree(rpc, session, NULL);
  }
  if (rpc_filter && data) {
    out = apply_filter_to_tree(ctx, data, rpc_filter);
    if (out != data) {
      lyd_free_all(data);
    }
  }
  if (!rpc_filter && !out && data) {
    out = data;
  }
  return reply_with_tree(rpc, session, out);
}

static struct nc_server_reply *
handle_get_config_rpc(struct lyd_node *rpc, struct nc_session *session) {
  const struct ly_ctx *ctx = nc_session_get_ctx(session);
  const struct lyd_node *source = find_descendant(rpc, "source");
  const struct lyd_node *ds_candidate =
      source ? child_by_name(source, "candidate") : NULL;
  const struct lyd_node *ds_running =
      source ? child_by_name(source, "running") : NULL;
  const char *path = ds_candidate ? g_candidate : g_running;
  struct lyd_node *data;
  struct lyd_node *out;
  struct lyd_node *rpc_filter = NULL;
  LY_ERR ret;

  if (source && !ds_candidate && !ds_running) {
    return rpc_error_msg(ctx, NC_ERR_INVALID_VALUE,
                         "Only source running/candidate is supported.");
  }
  if (!path || !path[0]) {
    return rpc_error_msg(
        ctx, NC_ERR_OP_NOT_SUPPORTED,
        "get-config is disabled (datastore artifacts not configured).");
  }

  data = load_json_file_tree(ctx, path);
  out = data;
  ret = lyd_find_path(rpc, "filter", 0, &rpc_filter);
  if (ret && (ret != LY_ENOTFOUND)) {
    lyd_free_all(data);
    return reply_with_tree(rpc, session, NULL);
  }
  if (rpc_filter && data) {
    out = apply_filter_to_tree(ctx, data, rpc_filter);
    if (out != data) {
      lyd_free_all(data);
    }
  }
  return reply_with_tree(rpc, session, out);
}

static int read_anyxml_payload(const struct lyd_node *node, char **out) {
  if (!node || !out) {
    return -1;
  }
  *out = NULL;
  if (lyd_any_value_str(node, out) || !*out || !(*out)[0]) {
    free(*out);
    *out = NULL;
    return -1;
  }
  return 0;
}

static int xml_payload_to_json(const struct ly_ctx *ctx, const char *xml,
                               char **json) {
  struct lyd_node *tree = NULL;
  int rc = -1;

  if (!ctx || !xml || !xml[0] || !json) {
    return -1;
  }
  *json = NULL;
  if (lyd_parse_data_mem(ctx, xml, LYD_XML, LYD_PARSE_STRICT, 0, &tree)) {
    lyd_free_all(tree);
    return -1;
  }
  if (!lyd_print_mem(json, tree, LYD_JSON, LYD_PRINT_SHRINK)) {
    rc = 0;
  }
  lyd_free_all(tree);
  return rc;
}

static int config_payload_to_json(const struct ly_ctx *ctx,
                                  const struct lyd_node *config,
                                  char **json) {
  const struct lyd_node_any *any;
  char *xml = NULL;
  int rc;

  if (!ctx || !config || !json) {
    return -1;
  }
  *json = NULL;
  if (config->schema &&
      ((config->schema->nodetype == LYS_ANYXML) ||
       (config->schema->nodetype == LYS_ANYDATA))) {
    any = (const struct lyd_node_any *)config;
    switch (any->value_type) {
    case LYD_ANYDATA_DATATREE:
      if (!any->value.tree) {
        return -1;
      }
      return lyd_print_mem(json, any->value.tree, LYD_JSON,
                           LYD_PRINT_SHRINK)
                 ? -1
                 : 0;
    case LYD_ANYDATA_JSON:
      if (!any->value.json || !any->value.json[0]) {
        return -1;
      }
      *json = strdup(any->value.json);
      return *json ? 0 : -1;
    case LYD_ANYDATA_XML:
      return xml_payload_to_json(ctx, any->value.xml, json);
    case LYD_ANYDATA_STRING:
    default:
      break;
    }
  }

  if (lyd_child(config)) {
    return lyd_print_mem(json, lyd_child(config), LYD_JSON, LYD_PRINT_SHRINK)
               ? -1
               : 0;
  }
  rc = read_anyxml_payload(config, &xml);
  if (rc) {
    return -1;
  }
  rc = xml_payload_to_json(ctx, xml, json);
  free(xml);
  return rc;
}

static struct nc_server_reply *control_status_reply(const struct ly_ctx *ctx,
                                                    int rc, long code,
                                                    char *resp, NC_ERR err_tag,
                                                    const char *fallback) {
  struct nc_server_reply *r;
  if ((rc == 0) && (code >= 200) && (code < 300)) {
    free(resp);
    return nc_server_reply_ok();
  }
  char *msg = extract_message(resp);
  if (!msg) {
    msg = strdup(fallback ? fallback : "internal control request failed");
  }
  r = rpc_error_msg(ctx, err_tag, msg ? msg : fallback);
  free(msg);
  free(resp);
  return r;
}

static struct nc_server_reply *
handle_edit_config_rpc(struct lyd_node *rpc, struct nc_session *session) {
  const struct ly_ctx *ctx = nc_session_get_ctx(session);
  const struct lyd_node *target = find_descendant(rpc, "target");
  const struct lyd_node *candidate =
      target ? child_by_name(target, "candidate") : NULL;
  const struct lyd_node *cfg = find_descendant(rpc, "config");
  const struct lyd_node *n;
  char *json = NULL;
  struct curl_slist *headers = NULL;
  long code = 0;
  char *resp = NULL;
  int rc;

  if (!g_control || !g_control[0]) {
    return rpc_error_msg(
        ctx, NC_ERR_OP_NOT_SUPPORTED,
        "edit-config is disabled (control endpoint not configured).");
  }
  if (!candidate) {
    return rpc_error_msg(ctx, NC_ERR_INVALID_VALUE,
                         "Only target candidate is supported.");
  }
  if (!cfg) {
    return rpc_error_msg(ctx, NC_ERR_MISSING_ELEM,
                         "edit-config requires config content.");
  }
  if (config_payload_to_json(ctx, cfg, &json)) {
    return rpc_error_msg(
        ctx, NC_ERR_INVALID_VALUE,
        "edit-config config content is not valid XML/YANG data.");
  }

  headers =
      curl_slist_append(headers, "Content-Type: application/yang-data+json");
  headers = curl_slist_append(headers, "Accept: application/json");
  headers = curl_slist_append(headers, "X-NETCONF-Target: candidate");
  if ((n = find_descendant(rpc, "default-operation")) && lyd_get_value(n) &&
      lyd_get_value(n)[0]) {
    char buf[128];
    snprintf(buf, sizeof(buf), "X-NETCONF-Default-Operation: %s",
             lyd_get_value(n));
    headers = curl_slist_append(headers, buf);
  }
  if ((n = find_descendant(rpc, "test-option")) && lyd_get_value(n) &&
      lyd_get_value(n)[0]) {
    char buf[128];
    snprintf(buf, sizeof(buf), "X-NETCONF-Test-Option: %s", lyd_get_value(n));
    headers = curl_slist_append(headers, buf);
  }
  if ((n = find_descendant(rpc, "error-option")) && lyd_get_value(n) &&
      lyd_get_value(n)[0]) {
    char buf[128];
    snprintf(buf, sizeof(buf), "X-NETCONF-Error-Option: %s", lyd_get_value(n));
    headers = curl_slist_append(headers, buf);
  }
  rc = control_post(session, "/v1/control/netconf/edit-config", json, 30L,
                    headers, &code, &resp);
  free(json);
  return control_status_reply(ctx, rc, code, resp, NC_ERR_OP_FAILED,
                              "edit-config failed");
}

static struct nc_server_reply *handle_validate_rpc(struct lyd_node *rpc,
                                                   struct nc_session *session) {
  const struct ly_ctx *ctx = nc_session_get_ctx(session);
  const struct lyd_node *source = find_descendant(rpc, "source");
  const struct lyd_node *config = find_descendant(rpc, "config");
  struct curl_slist *headers = NULL;
  char *json = NULL;
  long code = 0;
  char *resp = NULL;
  int rc;

  if (!g_control || !g_control[0]) {
    return rpc_error_msg(
        ctx, NC_ERR_OP_NOT_SUPPORTED,
        "validate is disabled (control endpoint not configured).");
  }
  headers = curl_slist_append(headers, "Accept: application/json");
  if (config) {
    if (config_payload_to_json(ctx, config, &json)) {
      curl_slist_free_all(headers);
      return rpc_error_msg(
          ctx, NC_ERR_INVALID_VALUE,
          "validate config content is not valid XML/YANG data.");
    }
    headers =
        curl_slist_append(headers, "Content-Type: application/yang-data+json");
  } else if (source) {
    const struct lyd_node *candidate = child_by_name(source, "candidate");
    const struct lyd_node *running = child_by_name(source, "running");
    if (candidate) {
      headers = curl_slist_append(headers, "X-NETCONF-Source: candidate");
    } else if (running) {
      headers = curl_slist_append(headers, "X-NETCONF-Source: running");
    } else {
      curl_slist_free_all(headers);
      return rpc_error_msg(ctx, NC_ERR_INVALID_VALUE,
                           "Only source running/candidate is supported.");
    }
  } else {
    headers = curl_slist_append(headers, "X-NETCONF-Source: candidate");
  }

  rc = control_post(session, "/v1/control/netconf/validate", json, 30L, headers,
                    &code, &resp);
  free(json);
  return control_status_reply(ctx, rc, code, resp, NC_ERR_OP_FAILED,
                              "validate failed");
}

static struct nc_server_reply *handle_commit_rpc(struct lyd_node *rpc,
                                                 struct nc_session *session) {
  const struct ly_ctx *ctx = nc_session_get_ctx(session);
  long code = 0;
  char *resp = NULL;
  int rc;
  (void)rpc;
  if (!g_control || !g_control[0]) {
    return rpc_error_msg(
        ctx, NC_ERR_OP_NOT_SUPPORTED,
        "commit is disabled (control endpoint not configured).");
  }
  rc = control_post(session, "/v1/control/netconf/commit", "{}", 180L, NULL,
                    &code, &resp);
  return control_status_reply(ctx, rc, code, resp, NC_ERR_OP_FAILED,
                              "commit failed");
}

static struct nc_server_reply *handle_discard_rpc(struct lyd_node *rpc,
                                                  struct nc_session *session) {
  const struct ly_ctx *ctx = nc_session_get_ctx(session);
  long code = 0;
  char *resp = NULL;
  int rc;
  (void)rpc;
  if (!g_control || !g_control[0]) {
    return rpc_error_msg(
        ctx, NC_ERR_OP_NOT_SUPPORTED,
        "discard-changes is disabled (control endpoint not configured).");
  }
  rc = control_post(session, "/v1/control/netconf/discard-changes", "{}", 30L,
                    NULL, &code, &resp);
  return control_status_reply(ctx, rc, code, resp, NC_ERR_OP_FAILED,
                              "discard-changes failed");
}

static struct nc_server_reply *handle_lock_like_rpc(struct lyd_node *rpc,
                                                    struct nc_session *session,
                                                    const char *path) {
  const struct ly_ctx *ctx = nc_session_get_ctx(session);
  const struct lyd_node *target = find_descendant(rpc, "target");
  const struct lyd_node *candidate =
      target ? child_by_name(target, "candidate") : NULL;
  const struct lyd_node *running =
      target ? child_by_name(target, "running") : NULL;
  struct curl_slist *headers = NULL;
  long code = 0;
  char *resp = NULL;
  int rc;

  if (!g_control || !g_control[0]) {
    return rpc_error_msg(
        ctx, NC_ERR_OP_NOT_SUPPORTED,
        "lock handling is disabled (control endpoint not configured).");
  }
  if (candidate) {
    headers = curl_slist_append(headers, "X-NETCONF-Target: candidate");
  } else if (running) {
    headers = curl_slist_append(headers, "X-NETCONF-Target: running");
  } else {
    return rpc_error_msg(ctx, NC_ERR_INVALID_VALUE,
                         "Only target running/candidate is supported.");
  }
  rc = control_post(session, path, "{}", 30L, headers, &code, &resp);
  return control_status_reply(ctx, rc, code, resp, NC_ERR_LOCK_DENIED, path);
}

static void notify_session_close(const struct nc_session *session) {
  long code = 0;
  char *resp = NULL;
  if (!g_control || !g_control[0] || !session) {
    return;
  }
  if (control_post(session, "/v1/control/netconf/session-close", "{}", 10L,
                   NULL, &code, &resp) != 0) {
    free(resp);
    return;
  }
  free(resp);
}

static struct nc_server_reply *handle_create_subscription_rpc(
    struct lyd_node *rpc, struct nc_session *session) {
  const struct ly_ctx *ctx = nc_session_get_ctx(session);
  struct session_state *st = find_session_state(session);
  const struct lyd_node *stop = find_descendant(rpc, "stopTime");

  if (stop && lyd_get_value(stop) && lyd_get_value(stop)[0]) {
    return rpc_error_msg(
        ctx, NC_ERR_OP_NOT_SUPPORTED,
        "stopTime subscriptions are not supported by this EMS stream.");
  }
  if (!st) {
    track_session(session);
    st = find_session_state(session);
  }
  if (!st) {
    return rpc_error_msg(ctx, NC_ERR_RES_DENIED,
                         "NETCONF session table is full.");
  }
  if (!st->subscribed) {
    nc_session_inc_notif_status(session);
    st->subscribed = 1;
  }
  return nc_server_reply_ok();
}

static int parse_notification_json(const struct ly_ctx *ctx, const char *json,
                                   struct lyd_node **event) {
  struct ly_in *in = NULL;
  struct lyd_node *rest_tree = NULL;
  struct lyd_node *op = NULL;
  LY_ERR ret;

  if (!ctx || !json || !json[0] || !event) {
    return -1;
  }
  *event = NULL;
  if (ly_in_new_memory(json, &in)) {
    return -1;
  }
  ret = lyd_parse_op(ctx, NULL, in, LYD_JSON, LYD_TYPE_NOTIF_YANG,
                     LYD_PARSE_STRICT, &rest_tree, &op);
  ly_in_free(in, 0);
  if (ret || !op) {
    lyd_free_all(rest_tree);
    lyd_free_all(op);
    return -1;
  }
  (void)rest_tree;
  *event = op;
  return 0;
}

static void send_notification_to_subscribers(const struct ly_ctx *ctx,
                                             const char *event_time,
                                             const char *json) {
  struct lyd_node *event = NULL;
  struct nc_server_notif *notif;
  char *ts;

  if (parse_notification_json(ctx, json, &event)) {
    return;
  }
  ts = strdup((event_time && event_time[0]) ? event_time : "1970-01-01T00:00:00Z");
  if (!ts) {
    lyd_free_all(event);
    return;
  }
  notif = nc_server_notif_new(event, ts, NC_PARAMTYPE_FREE);
  if (!notif) {
    lyd_free_all(event);
    free(ts);
    return;
  }

  for (size_t i = 0; i < MAX_TRACKED_SESSIONS; ++i) {
    if (!g_sessions[i].session || !g_sessions[i].subscribed) {
      continue;
    }
    if (nc_session_get_notif_status(g_sessions[i].session) <= 0) {
      continue;
    }
    (void)nc_server_notif_send(g_sessions[i].session, notif, 100);
  }
  nc_server_notif_free(notif);
}

static void poll_and_dispatch_notifications(const struct ly_ctx *ctx) {
  long code = 0;
  char *resp = NULL;
  char *saveptr = NULL;
  char *line;

  if (!ctx || !g_control || !g_control[0]) {
    return;
  }
  if (control_post_system("/v1/control/netconf/notifications?max=100", "",
                          2L, &code, &resp) != 0) {
    free(resp);
    return;
  }
  if (code == 204 || !resp || !resp[0]) {
    free(resp);
    return;
  }
  if (code < 200 || code >= 300) {
    free(resp);
    return;
  }
  if (!any_subscribed_session()) {
    free(resp);
    return;
  }

  for (line = strtok_r(resp, "\n", &saveptr); line;
       line = strtok_r(NULL, "\n", &saveptr)) {
    char *tab = strchr(line, '\t');
    if (!tab) {
      continue;
    }
    *tab = '\0';
    send_notification_to_subscribers(ctx, line, tab + 1);
  }
  free(resp);
}

static struct nc_server_reply *rpc_cb(struct lyd_node *rpc,
                                      struct nc_session *session) {
  const char *rpc_name = LYD_NAME(rpc);
  const char *rpc_mod = lyd_owner_module(rpc)->name;

  if (!strcmp(rpc_name, "close-session") && !strcmp(rpc_mod, "ietf-netconf")) {
    notify_session_close(session);
    return nc_clb_default_close_session(rpc, session);
  }
  if (!strcmp(rpc_name, "get-schema") &&
      !strcmp(rpc_mod, "ietf-netconf-monitoring")) {
    return nc_clb_default_get_schema(rpc, session);
  }
  if (!strcmp(rpc_name, "create-subscription") &&
      !strcmp(rpc_mod, "notifications")) {
    return handle_create_subscription_rpc(rpc, session);
  }
  if (!strcmp(rpc_name, "get") && !strcmp(rpc_mod, "ietf-netconf")) {
    return handle_get_rpc(rpc, session);
  }
  if (!strcmp(rpc_name, "get-config") && !strcmp(rpc_mod, "ietf-netconf")) {
    return handle_get_config_rpc(rpc, session);
  }
  if (!strcmp(rpc_name, "edit-config") && !strcmp(rpc_mod, "ietf-netconf")) {
    return handle_edit_config_rpc(rpc, session);
  }
  if (!strcmp(rpc_name, "validate") && !strcmp(rpc_mod, "ietf-netconf")) {
    return handle_validate_rpc(rpc, session);
  }
  if (!strcmp(rpc_name, "commit") && !strcmp(rpc_mod, "ietf-netconf")) {
    return handle_commit_rpc(rpc, session);
  }
  if (!strcmp(rpc_name, "discard-changes") &&
      !strcmp(rpc_mod, "ietf-netconf")) {
    return handle_discard_rpc(rpc, session);
  }
  if (!strcmp(rpc_name, "lock") && !strcmp(rpc_mod, "ietf-netconf")) {
    return handle_lock_like_rpc(rpc, session, "/v1/control/netconf/lock");
  }
  if (!strcmp(rpc_name, "unlock") && !strcmp(rpc_mod, "ietf-netconf")) {
    return handle_lock_like_rpc(rpc, session, "/v1/control/netconf/unlock");
  }
  return nc_server_reply_ok();
}

static void usage(const char *prog) {
  fprintf(stderr,
          "Usage: %s -addr <host:port> -yang <dir[:dir...]> -snapshot <path> "
          "-running <path> -candidate <path> -hostkey <path> -authorized-key "
          "<path> -user <name[,name...]> [-control <url>] [-control-unix "
          "<sockpath>]\n",
          prog);
}

int main(int argc, char **argv) {
  const char *addr = NULL;
  const char *yang_dir = NULL;
  const char *hostkey = NULL;
  const char *auth_key = NULL;
  const char *user = "admin";
  char *addr_copy = NULL;
  char *host = NULL;
  char *port_str = NULL;
  uint16_t port;
  struct ly_ctx *ctx = NULL;
  struct lyd_node *config = NULL;
  struct nc_pollsession *ps = NULL;

  for (int i = 1; i < argc; ++i) {
    if (!strcmp(argv[i], "-addr") && (i + 1 < argc)) {
      addr = argv[++i];
    } else if (!strcmp(argv[i], "-yang") && (i + 1 < argc)) {
      yang_dir = argv[++i];
    } else if (!strcmp(argv[i], "-snapshot") && (i + 1 < argc)) {
      g_snapshot = argv[++i];
    } else if (!strcmp(argv[i], "-running") && (i + 1 < argc)) {
      g_running = argv[++i];
    } else if (!strcmp(argv[i], "-candidate") && (i + 1 < argc)) {
      g_candidate = argv[++i];
    } else if (!strcmp(argv[i], "-hostkey") && (i + 1 < argc)) {
      hostkey = argv[++i];
    } else if (!strcmp(argv[i], "-authorized-key") && (i + 1 < argc)) {
      auth_key = argv[++i];
    } else if (!strcmp(argv[i], "-user") && (i + 1 < argc)) {
      user = argv[++i];
    } else if (!strcmp(argv[i], "-control") && (i + 1 < argc)) {
      g_control = argv[++i];
    } else if (!strcmp(argv[i], "-control-unix") && (i + 1 < argc)) {
      g_control_unix = argv[++i];
    } else if (!strcmp(argv[i], "-h") || !strcmp(argv[i], "--help")) {
      usage(argv[0]);
      return 0;
    }
  }

  if (!addr || !yang_dir || !g_snapshot || !hostkey || !auth_key) {
    usage(argv[0]);
    return 1;
  }

  addr_copy = strdup(addr);
  if (!addr_copy) {
    return 1;
  }
  host = addr_copy;
  port_str = strchr(addr_copy, ':');
  if (!port_str) {
    free(addr_copy);
    return 1;
  }
  *port_str = '\0';
  ++port_str;
  port = (uint16_t)atoi(port_str);

  signal(SIGINT, on_sigint);
  signal(SIGTERM, on_sigint);
  curl_global_init(CURL_GLOBAL_DEFAULT);

  if (nc_server_init()) {
    free(addr_copy);
    curl_global_cleanup();
    return 1;
  }
  if (nc_server_init_ctx(&ctx)) {
    free(addr_copy);
    nc_server_destroy();
    curl_global_cleanup();
    return 1;
  }
  if (nc_server_config_load_modules(&ctx)) {
    free(addr_copy);
    nc_server_destroy();
    ly_ctx_destroy(ctx);
    curl_global_cleanup();
    return 1;
  }
  if (load_yang_dir_modules(ctx, yang_dir)) {
    free(addr_copy);
    nc_server_destroy();
    ly_ctx_destroy(ctx);
    curl_global_cleanup();
    return 1;
  }
  const char *netconf_features[] = {"candidate", "xpath", "validate", NULL};
  (void)ly_ctx_load_module(ctx, "ietf-netconf", NULL, netconf_features);
  (void)ly_ctx_load_module(ctx, "notifications", NULL, NULL);
  (void)nc_server_set_capability(
      "urn:ietf:params:netconf:capability:notification:1.0");

  if (nc_server_config_add_address_port(ctx, "ems-ssh", NC_TI_SSH, host, port,
                                        &config)) {
    free(addr_copy);
    nc_server_destroy();
    ly_ctx_destroy(ctx);
    curl_global_cleanup();
    return 1;
  }
  if (nc_server_config_add_ssh_hostkey(ctx, "ems-ssh", "hostkey", hostkey, NULL,
                                       &config)) {
    free(addr_copy);
    nc_server_destroy();
    ly_ctx_destroy(ctx);
    curl_global_cleanup();
    return 1;
  }

  char *users_copy = strdup(user);
  int added_user = 0;
  if (!users_copy) {
    free(addr_copy);
    nc_server_destroy();
    ly_ctx_destroy(ctx);
    curl_global_cleanup();
    return 1;
  }
  for (char *tok = strtok(users_copy, ","); tok; tok = strtok(NULL, ",")) {
    while (*tok == ' ') {
      ++tok;
    }
    if (!*tok) {
      continue;
    }
    if (nc_server_config_add_ssh_user_pubkey(ctx, "ems-ssh", tok, "nms-key",
                                             auth_key, &config)) {
      free(users_copy);
      free(addr_copy);
      nc_server_destroy();
      ly_ctx_destroy(ctx);
      curl_global_cleanup();
      return 1;
    }
    added_user = 1;
  }
  free(users_copy);
  if (!added_user) {
    free(addr_copy);
    nc_server_destroy();
    ly_ctx_destroy(ctx);
    curl_global_cleanup();
    return 1;
  }
  if (nc_server_config_setup_data(config)) {
    free(addr_copy);
    lyd_free_all(config);
    nc_server_destroy();
    ly_ctx_destroy(ctx);
    curl_global_cleanup();
    return 1;
  }
  lyd_free_all(config);

  nc_set_global_rpc_clb(rpc_cb);
  ps = nc_ps_new();
  if (!ps) {
    free(addr_copy);
    nc_server_destroy();
    ly_ctx_destroy(ctx);
    curl_global_cleanup();
    return 1;
  }

  while (!g_stop) {
    struct nc_session *session = NULL;
    int acc = nc_accept(100, ctx, &session);
    if (acc == NC_MSG_HELLO) {
      if (nc_ps_add_session(ps, session)) {
        nc_session_free(session, NULL);
      } else {
        track_session(session);
      }
    }
    int ret = nc_ps_poll(ps, 100, &session);
    if (ret & NC_PSPOLL_SESSION_TERM) {
      if (session) {
        notify_session_close(session);
        untrack_session(session);
        nc_ps_del_session(ps, session);
        nc_session_free(session, NULL);
      }
    }
    poll_and_dispatch_notifications(ctx);
  }

  nc_ps_free(ps);
  nc_server_destroy();
  ly_ctx_destroy(ctx);
  curl_global_cleanup();
  free(addr_copy);
  return 0;
}
