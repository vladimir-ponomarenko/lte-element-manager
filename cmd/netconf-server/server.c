#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <time.h>

#include <curl/curl.h>
#include <openssl/sha.h>

#include <libyang/libyang.h>
#include <nc_server.h>

static volatile sig_atomic_t g_stop = 0;
static const char *g_snapshot = NULL;
static const char *g_control = NULL;

static const char *g_mod_legacy = "ems-enb-metrics";
static const char *g_mod_common = "_3gpp-common-managed-element";
static const char *g_mod_vendor = "srsran-vendor-ext";

static void sha256_hex(const char *data, char out[65]) {
    unsigned char hash[SHA256_DIGEST_LENGTH];
    SHA256((const unsigned char *)data, strlen(data), hash);
    for (int i = 0; i < SHA256_DIGEST_LENGTH; i++) {
        sprintf(out + (i * 2), "%02x", hash[i]);
    }
    out[64] = '\0';
}

static void emit_netconf_get(const struct nc_session *session, const char *raw) {
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

    fprintf(stdout, "NETCONF_GET user=%s ts=%s bytes=%zu sha256=%s", user ? user : "unknown", ts, bytes, sha);
    if (bytes <= 16384) {
        fprintf(stdout, " json=%s", raw);
    }
    fprintf(stdout, "\n");
    fflush(stdout);
}

static void on_sigint(int sig) {
    (void)sig;
    g_stop = 1;
}

static char *read_file(const char *path) {
    FILE *f = fopen(path, "rb");
    if (!f) {
        return NULL;
    }
    if (fseek(f, 0, SEEK_END) != 0) {
        fclose(f);
        return NULL;
    }
    long size = ftell(f);
    if (size < 0) {
        fclose(f);
        return NULL;
    }
    rewind(f);
    char *buf = (char *)malloc((size_t)size + 1);
    if (!buf) {
        fclose(f);
        return NULL;
    }
    size_t n = fread(buf, 1, (size_t)size, f);
    fclose(f);
    buf[n] = '\0';
    return buf;
}

static struct lyd_node *load_datastore_tree(const struct ly_ctx *ctx, const char *raw) {
    if (!raw) {
        return NULL;
    }

    struct lyd_node *tree = NULL;
    if (lyd_parse_data_mem(ctx, raw, LYD_JSON, LYD_PARSE_STRICT, LYD_VALIDATE_PRESENT, &tree)) {
        return NULL;
    }
    return tree;
}

static const char *meta_value_by_name(const struct lyd_node *n, const char *name) {
    struct lyd_meta *m;
    LY_LIST_FOR(n->meta, m) {
        if (!strcmp(m->name, name)) {
            return lyd_get_meta_value(m);
        }
    }
    return NULL;
}

static const struct lyd_node *child_by_name(const struct lyd_node *parent, const char *name) {
    const struct lyd_node *ch;
    LY_LIST_FOR(lyd_child(parent), ch) {
        if (!strcmp(LYD_NAME(ch), name)) {
            return ch;
        }
    }
    return NULL;
}

static const char *child_value(const struct lyd_node *parent, const char *leaf) {
    const struct lyd_node *n = child_by_name(parent, leaf);
    if (!n) {
        return NULL;
    }
    return lyd_get_value(n);
}

static char *subtree_filter_to_xpath(const struct ly_ctx *ctx, const char *xml) {
    if (!xml || !xml[0]) {
        return NULL;
    }

    struct lyd_node *ft = NULL;
    if (lyd_parse_data_mem(ctx, xml, LYD_XML, LYD_PARSE_ONLY, 0, &ft)) {
        lyd_free_siblings(ft);
        return NULL;
    }

    /* Legacy path. */
    if (ft && !strcmp(LYD_NAME(ft), "enb_metrics") && !strcmp(lyd_owner_module(ft)->name, g_mod_legacy)) {
        lyd_free_siblings(ft);
        return strdup("/ems-enb-metrics:enb_metrics");
    }

    /* Common NRM tree. */
    const struct lyd_node *sn = ft;
    if (sn && strcmp(LYD_NAME(sn), "SubNetwork")) {
        sn = child_by_name(ft, "SubNetwork");
    }
    if (!sn || strcmp(LYD_NAME(sn), "SubNetwork") || strcmp(lyd_owner_module(sn)->name, g_mod_common)) {
        lyd_free_siblings(ft);
        return NULL;
    }

    const char *sn_id = child_value(sn, "id");
    const struct lyd_node *me = child_by_name(sn, "ManagedElement");
    const char *me_id = me ? child_value(me, "id") : NULL;
    const struct lyd_node *fn = me ? child_by_name(me, "ENBFunction") : NULL;
    const char *fn_id = fn ? child_value(fn, "id") : NULL;
    const struct lyd_node *cell = fn ? child_by_name(fn, "EUtranCell") : NULL;
    const char *cell_id = cell ? child_value(cell, "id") : NULL;
    const struct lyd_node *meas = cell ? child_by_name(cell, "measurements") : NULL;

    char xpath[1024];
    size_t off = 0;
    off += (size_t)snprintf(xpath + off, sizeof(xpath) - off, "/_3gpp-common-managed-element:SubNetwork");
    if (sn_id && sn_id[0]) {
        off += (size_t)snprintf(xpath + off, sizeof(xpath) - off, "[id='%s']", sn_id);
    }
    if (me) {
        off += (size_t)snprintf(xpath + off, sizeof(xpath) - off, "/_3gpp-common-managed-element:ManagedElement");
        if (me_id && me_id[0]) {
            off += (size_t)snprintf(xpath + off, sizeof(xpath) - off, "[id='%s']", me_id);
        }
    }
    if (fn) {
        off += (size_t)snprintf(xpath + off, sizeof(xpath) - off, "/_3gpp-common-managed-element:ENBFunction");
        if (fn_id && fn_id[0]) {
            off += (size_t)snprintf(xpath + off, sizeof(xpath) - off, "[id='%s']", fn_id);
        }
    }
    if (cell) {
        off += (size_t)snprintf(xpath + off, sizeof(xpath) - off, "/_3gpp-common-managed-element:EUtranCell");
        if (cell_id && cell_id[0]) {
            off += (size_t)snprintf(xpath + off, sizeof(xpath) - off, "[id='%s']", cell_id);
        }
        if (meas) {
            off += (size_t)snprintf(xpath + off, sizeof(xpath) - off, "/_3gpp-common-managed-element:measurements");
        }
    }

    lyd_free_siblings(ft);
    return strdup(xpath);
}

static struct lyd_node *filter_by_xpath(struct lyd_node *root, const char *xpath) {
    struct ly_set *set = NULL;
    if (!root || !xpath || !xpath[0]) {
        return root;
    }
    if (lyd_find_xpath(root, xpath, &set)) {
        ly_set_free(set, NULL);
        return NULL;
    }

    struct lyd_node *out = NULL;
    for (uint32_t i = 0; i < set->count; i++) {
        struct lyd_node *dup = NULL;
        if (lyd_dup_single(set->dnodes[i], NULL, LYD_DUP_RECURSIVE | LYD_DUP_WITH_PARENTS, &dup)) {
            lyd_free_siblings(out);
            ly_set_free(set, NULL);
            return NULL;
        }
        while (dup->parent) {
            dup = lyd_parent(dup);
        }
        if (lyd_merge_tree(&out, dup, LYD_MERGE_DESTRUCT)) {
            lyd_free_siblings(dup);
            lyd_free_siblings(out);
            ly_set_free(set, NULL);
            return NULL;
        }
    }

    ly_set_free(set, NULL);
    return out;
}

struct http_buf {
    char *ptr;
    size_t len;
};

struct edit_values {
    char *enb_serial;
    char *mcc;
    char *mnc;
    char *n_prb;
    char *tx_gain;
    char *dl_earfcn;
    char *pci;
};

static void free_edit_values(struct edit_values *v) {
    if (!v) {
        return;
    }
    free(v->enb_serial);
    free(v->mcc);
    free(v->mnc);
    free(v->n_prb);
    free(v->tx_gain);
    free(v->dl_earfcn);
    free(v->pci);
}

static void set_str(char **dst, const char *src) {
    if (!dst || !src) {
        return;
    }
    free(*dst);
    *dst = strdup(src);
}

static size_t curl_write_cb(char *contents, size_t size, size_t nmemb, void *userdata) {
    size_t n = size * nmemb;
    struct http_buf *b = (struct http_buf *)userdata;
    char *p = realloc(b->ptr, b->len + n + 1);
    if (!p) {
        return 0;
    }
    b->ptr = p;
    memcpy(b->ptr + b->len, contents, n);
    b->len += n;
    b->ptr[b->len] = '\0';
    return n;
}

static int http_post_json(const char *url, const char *payload, long *status, char **response) {
    CURL *curl = curl_easy_init();
    if (!curl) {
        return -1;
    }

    struct http_buf b = {.ptr = NULL, .len = 0};
    struct curl_slist *headers = NULL;
    headers = curl_slist_append(headers, "Content-Type: application/json");

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_POST, 1L);
    curl_easy_setopt(curl, CURLOPT_POSTFIELDS, payload ? payload : "{}");
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 15L);
    curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 2L);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curl_write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &b);

    CURLcode rc = curl_easy_perform(curl);
    if (rc != CURLE_OK) {
        curl_slist_free_all(headers);
        curl_easy_cleanup(curl);
        free(b.ptr);
        return -1;
    }

    long code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &code);
    if (status) {
        *status = code;
    }
    if (response) {
        *response = b.ptr;
        b.ptr = NULL;
    }

    free(b.ptr);
    curl_slist_free_all(headers);
    curl_easy_cleanup(curl);
    return 0;
}

static int http_post_json_timeout(const char *url, const char *payload, long timeout_sec, long *status, char **response) {
    CURL *curl = curl_easy_init();
    if (!curl) {
        return -1;
    }

    struct http_buf b = {.ptr = NULL, .len = 0};
    struct curl_slist *headers = NULL;
    headers = curl_slist_append(headers, "Content-Type: application/json");

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_POST, 1L);
    curl_easy_setopt(curl, CURLOPT_POSTFIELDS, payload ? payload : "{}");
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, timeout_sec);
    curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 2L);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curl_write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &b);

    CURLcode rc = curl_easy_perform(curl);
    if (rc != CURLE_OK) {
        curl_slist_free_all(headers);
        curl_easy_cleanup(curl);
        free(b.ptr);
        return -1;
    }

    long code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &code);
    if (status) {
        *status = code;
    }
    if (response) {
        *response = b.ptr;
        b.ptr = NULL;
    }

    free(b.ptr);
    curl_slist_free_all(headers);
    curl_easy_cleanup(curl);
    return 0;
}

static int http_get_json(const char *url, long *status, char **response) {
    CURL *curl = curl_easy_init();
    if (!curl) {
        return -1;
    }

    struct http_buf b = {.ptr = NULL, .len = 0};
    struct curl_slist *headers = NULL;
    headers = curl_slist_append(headers, "Accept: application/json");

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_HTTPGET, 1L);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 15L);
    curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 2L);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curl_write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &b);

    CURLcode rc = curl_easy_perform(curl);
    if (rc != CURLE_OK) {
        curl_slist_free_all(headers);
        curl_easy_cleanup(curl);
        free(b.ptr);
        return -1;
    }

    long code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &code);
    if (status) {
        *status = code;
    }
    if (response) {
        *response = b.ptr;
        b.ptr = NULL;
    }

    free(b.ptr);
    curl_slist_free_all(headers);
    curl_easy_cleanup(curl);
    return 0;
}

static int is_supported_leaf(const char *name) {
    return !strcmp(name, "enb_serial") || !strcmp(name, "mcc") || !strcmp(name, "mnc") || !strcmp(name, "n_prb") ||
           !strcmp(name, "tx_gain") || !strcmp(name, "dl_earfcn") || !strcmp(name, "pci");
}

static int is_numeric_leaf(const char *name) {
    return !strcmp(name, "n_prb") || !strcmp(name, "tx_gain") || !strcmp(name, "dl_earfcn") || !strcmp(name, "pci");
}

static void collect_edit_values(const struct lyd_node *n, struct edit_values *vals) {
    if (!n || !vals) {
        return;
    }

    const struct lyd_node *it;
    LY_LIST_FOR(n, it) {
        const char *name = LYD_NAME(it);
        const char *val = lyd_get_value(it);
        if (name && val && is_supported_leaf(name)) {
            if (!strcmp(name, "enb_serial")) {
                set_str(&vals->enb_serial, val);
            } else if (!strcmp(name, "mcc")) {
                set_str(&vals->mcc, val);
            } else if (!strcmp(name, "mnc")) {
                set_str(&vals->mnc, val);
            } else if (!strcmp(name, "n_prb")) {
                set_str(&vals->n_prb, val);
            } else if (!strcmp(name, "tx_gain")) {
                set_str(&vals->tx_gain, val);
            } else if (!strcmp(name, "dl_earfcn")) {
                set_str(&vals->dl_earfcn, val);
            } else if (!strcmp(name, "pci")) {
                set_str(&vals->pci, val);
            }
        }
        collect_edit_values(lyd_child(it), vals);
    }
}

static char *json_escape(const char *s) {
    if (!s) {
        return strdup("");
    }
    size_t len = strlen(s);
    char *out = malloc((len * 2) + 1);
    if (!out) {
        return NULL;
    }
    size_t j = 0;
    for (size_t i = 0; i < len; i++) {
        if (s[i] == '\\' || s[i] == '"') {
            out[j++] = '\\';
        }
        out[j++] = s[i];
    }
    out[j] = '\0';
    return out;
}

static int append_change(char *buf, size_t cap, size_t *off, const char *key, const char *val, int numeric, int *first) {
    if (!val || !val[0]) {
        return 0;
    }
    int n = 0;
    if (!*first) {
        n = snprintf(buf + *off, cap - *off, ",");
        if (n < 0 || *off + (size_t)n >= cap) {
            return -1;
        }
        *off += (size_t)n;
    }
    *first = 0;

    if (numeric) {
        n = snprintf(buf + *off, cap - *off, "\"%s\":%s", key, val);
        if (n < 0 || *off + (size_t)n >= cap) {
            return -1;
        }
        *off += (size_t)n;
        return 0;
    }

    char *esc = json_escape(val);
    if (!esc) {
        return -1;
    }
    n = snprintf(buf + *off, cap - *off, "\"%s\":\"%s\"", key, esc);
    free(esc);
    if (n < 0 || *off + (size_t)n >= cap) {
        return -1;
    }
    *off += (size_t)n;
    return 0;
}

static char *build_edit_payload(struct edit_values *vals) {
    char *buf = calloc(1, 8192);
    if (!buf) {
        return NULL;
    }
    size_t off = 0;
    int first = 1;
    int n = snprintf(buf + off, 8192 - off, "{\"changes\":{");
    if (n < 0 || off + (size_t)n >= 8192) {
        free(buf);
        return NULL;
    }
    off += (size_t)n;

    if (append_change(buf, 8192, &off, "enb_serial", vals->enb_serial, 0, &first) ||
        append_change(buf, 8192, &off, "mcc", vals->mcc, 0, &first) ||
        append_change(buf, 8192, &off, "mnc", vals->mnc, 0, &first) ||
        append_change(buf, 8192, &off, "n_prb", vals->n_prb, 1, &first) ||
        append_change(buf, 8192, &off, "tx_gain", vals->tx_gain, 1, &first) ||
        append_change(buf, 8192, &off, "dl_earfcn", vals->dl_earfcn, 1, &first) ||
        append_change(buf, 8192, &off, "pci", vals->pci, 1, &first)) {
        free(buf);
        return NULL;
    }
    n = snprintf(buf + off, 8192 - off, "}}");
    if (n < 0 || off + (size_t)n >= 8192) {
        free(buf);
        return NULL;
    }
    off += (size_t)n;
    if (first) {
        free(buf);
        return NULL;
    }
    return buf;
}

static char *extract_message(const char *json) {
    if (!json) {
        return NULL;
    }
    const char *p = strstr(json, "\"message\"");
    if (!p) {
        return NULL;
    }
    p = strchr(p, ':');
    if (!p) {
        return NULL;
    }
    p++;
    while (*p == ' ' || *p == '\t') {
        p++;
    }
    if (*p != '"') {
        return NULL;
    }
    p++;
    const char *q = p;
    while (*q && *q != '"') {
        if (*q == '\\' && *(q + 1)) {
            q += 2;
            continue;
        }
        q++;
    }
    if (*q != '"') {
        return NULL;
    }
    size_t len = (size_t)(q - p);
    char *out = malloc(len + 1);
    if (!out) {
        return NULL;
    }
    memcpy(out, p, len);
    out[len] = '\0';
    return out;
}

static char *extract_kv_value(const char *json, const char *key) {
    if (!json || !key || !key[0]) {
        return NULL;
    }

    char needle[128];
    snprintf(needle, sizeof(needle), "\"%s\"", key);
    const char *p = strstr(json, needle);
    if (!p) {
        return NULL;
    }
    p = strchr(p, ':');
    if (!p) {
        return NULL;
    }
    p++;
    while (*p == ' ' || *p == '\t') {
        p++;
    }
    if (*p == '"') {
        p++;
        const char *q = p;
        while (*q && *q != '"') {
            if (*q == '\\' && *(q + 1)) {
                q += 2;
                continue;
            }
            q++;
        }
        if (*q != '"') {
            return NULL;
        }
        size_t len = (size_t)(q - p);
        char *out = malloc(len + 1);
        if (!out) {
            return NULL;
        }
        memcpy(out, p, len);
        out[len] = '\0';
        return out;
    }

    /* number */
    const char *q = p;
    while (*q && ((*q >= '0' && *q <= '9') || *q == '.' || *q == '-' || *q == '+')) {
        q++;
    }
    if (q == p) {
        return NULL;
    }
    size_t len = (size_t)(q - p);
    char *out = malloc(len + 1);
    if (!out) {
        return NULL;
    }
    memcpy(out, p, len);
    out[len] = '\0';
    return out;
}

static struct nc_server_reply *rpc_error_msg(const struct ly_ctx *ctx, NC_ERR tag, const char *msg) {
    struct lyd_node *err = nc_err(ctx, tag, NC_ERR_TYPE_APP);
    if (err && msg && msg[0]) {
        nc_err_set_msg(err, msg, "en");
    }
    return nc_server_reply_err(err);
}

static const struct lyd_node *find_descendant(const struct lyd_node *root, const char *name) {
    if (!root) {
        return NULL;
    }
    const struct lyd_node *it;
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

static struct nc_server_reply *handle_edit_config_rpc(const struct ly_ctx *ctx, struct lyd_node *rpc) {
    if (!g_control || !g_control[0]) {
        return rpc_error_msg(ctx, NC_ERR_OP_NOT_SUPPORTED, "edit-config is disabled (control endpoint not configured).");
    }

    const struct lyd_node *target = find_descendant(rpc, "target");
    const struct lyd_node *candidate = target ? child_by_name(target, "candidate") : NULL;
    if (!candidate) {
        return rpc_error_msg(ctx, NC_ERR_INVALID_VALUE, "Only target candidate is supported.");
    }

    const struct lyd_node *cfg = find_descendant(rpc, "config");
    if (!cfg) {
        return rpc_error_msg(ctx, NC_ERR_MISSING_ELEM, "edit-config requires config content.");
    }

    char *xml = NULL;
    if (lyd_any_value_str(cfg, &xml) || !xml || !xml[0]) {
        free(xml);
        return rpc_error_msg(ctx, NC_ERR_INVALID_VALUE, "edit-config config content is empty.");
    }
    struct lyd_node *edit_tree = NULL;
    if (lyd_parse_data_mem(ctx, xml, LYD_XML, LYD_PARSE_ONLY, 0, &edit_tree)) {
        free(xml);
        lyd_free_siblings(edit_tree);
        return rpc_error_msg(ctx, NC_ERR_INVALID_VALUE, "edit-config config content is not valid XML/YANG data.");
    }
    free(xml);

    struct edit_values vals = {0};
    collect_edit_values(edit_tree, &vals);
    lyd_free_siblings(edit_tree);
    char *payload = build_edit_payload(&vals);
    free_edit_values(&vals);
    if (!payload) {
        return rpc_error_msg(ctx, NC_ERR_INVALID_VALUE, "No supported editable leaves found in edit-config.");
    }

    char url[512];
    snprintf(url, sizeof(url), "%s/v1/control/config/edit-config", g_control);
    long code = 0;
    char *resp = NULL;
    int rc = http_post_json(url, payload, &code, &resp);
    free(payload);
    if (rc != 0) {
        free(resp);
        return rpc_error_msg(ctx, NC_ERR_OP_FAILED, "Failed to call internal edit-config endpoint.");
    }
    if (code < 200 || code >= 300 || !resp || !strstr(resp, "\"status\":\"ok\"")) {
        char *msg = extract_message(resp);
        if (!msg) {
            msg = strdup("edit-config failed");
        }
        struct nc_server_reply *r = rpc_error_msg(ctx, NC_ERR_OP_FAILED, msg ? msg : "edit-config failed");
        free(msg);
        free(resp);
        return r;
    }
    free(resp);
    return nc_server_reply_ok();
}

static struct nc_server_reply *handle_commit_rpc(const struct ly_ctx *ctx) {
    if (!g_control || !g_control[0]) {
        return rpc_error_msg(ctx, NC_ERR_OP_NOT_SUPPORTED, "commit is disabled (control endpoint not configured).");
    }
    char url[512];
    snprintf(url, sizeof(url), "%s/v1/control/config/commit", g_control);
    long code = 0;
    char *resp = NULL;
    int rc = http_post_json_timeout(url, "{}", 180L, &code, &resp);
    if (rc != 0) {
        free(resp);
        return rpc_error_msg(ctx, NC_ERR_OP_FAILED, "Failed to call internal commit endpoint.");
    }
    if (code < 200 || code >= 300 || !resp || !strstr(resp, "\"status\":\"ok\"")) {
        char *msg = extract_message(resp);
        if (!msg) {
            msg = strdup("commit failed");
        }
        struct nc_server_reply *r = rpc_error_msg(ctx, NC_ERR_OP_FAILED, msg ? msg : "commit failed");
        free(msg);
        free(resp);
        return r;
    }
    free(resp);
    return nc_server_reply_ok();
}

static char *first_xpath_value(struct lyd_node *root, const char *xpath) {
    struct ly_set *set = NULL;
    if (!root || !xpath || !xpath[0]) {
        return NULL;
    }
    if (lyd_find_xpath(root, xpath, &set)) {
        ly_set_free(set, NULL);
        return NULL;
    }
    if (!set || (set->count == 0) || !set->dnodes[0]) {
        ly_set_free(set, NULL);
        return NULL;
    }
    const char *v = lyd_get_value(set->dnodes[0]);
    char *out = v ? strdup(v) : NULL;
    ly_set_free(set, NULL);
    return out;
}

static struct lyd_node *build_config_tree(const struct ly_ctx *ctx, struct lyd_node *snapshot_tree, const char *json) {
    if (!ctx || !json) {
        return NULL;
    }

    char *sn_id = first_xpath_value(snapshot_tree, "/_3gpp-common-managed-element:SubNetwork/id");
    char *me_id = first_xpath_value(snapshot_tree, "/_3gpp-common-managed-element:SubNetwork/ManagedElement/id");
    char *fn_id = first_xpath_value(snapshot_tree, "/_3gpp-common-managed-element:SubNetwork/ManagedElement/ENBFunction/id");
    char *cell_id = first_xpath_value(snapshot_tree, "/_3gpp-common-managed-element:SubNetwork/ManagedElement/ENBFunction/EUtranCell/id");

    if (!sn_id) sn_id = strdup("srsRAN");
    if (!me_id) me_id = strdup("enb1");
    if (!fn_id) fn_id = strdup("1");
    if (!cell_id) cell_id = strdup("1");

    char *mcc = extract_kv_value(json, "mcc");
    char *mnc = extract_kv_value(json, "mnc");
    char *n_prb = extract_kv_value(json, "n_prb");
    char *dl_earfcn = extract_kv_value(json, "dl_earfcn");
    char *pci = extract_kv_value(json, "pci");
    char *enb_serial = extract_kv_value(json, "enb_serial");
    char *tx_gain = extract_kv_value(json, "tx_gain");

    struct lyd_node *tree = NULL;
    char path[1024];

    snprintf(path, sizeof(path), "/_3gpp-common-managed-element:SubNetwork[id='%s']", sn_id);
    if (lyd_new_path(NULL, ctx, path, NULL, 0, &tree)) {
        goto cleanup;
    }
    snprintf(path, sizeof(path), "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']",
             sn_id, me_id);
    if (lyd_new_path(tree, ctx, path, NULL, 0, NULL)) {
        goto cleanup;
    }
    snprintf(path, sizeof(path),
             "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']",
             sn_id, me_id, fn_id);
    if (lyd_new_path(tree, ctx, path, NULL, 0, NULL)) {
        goto cleanup;
    }

    if (mcc) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:mcc",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, mcc, 0, NULL);
    }
    if (mnc) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:mnc",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, mnc, 0, NULL);
    }
    if (n_prb) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:n_prb",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, n_prb, 0, NULL);
    }

    if (enb_serial) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:enb_serial",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, enb_serial, 0, NULL);
    }
    if (tx_gain) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:tx_gain",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, tx_gain, 0, NULL);
    }

    snprintf(path, sizeof(path),
             "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:EUtranCell[id='%s']",
             sn_id, me_id, fn_id, cell_id);
    lyd_new_path(tree, ctx, path, NULL, 0, NULL);

    if (dl_earfcn) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:EUtranCell[id='%s']/_3gpp-common-managed-element:dl_earfcn",
                 sn_id, me_id, fn_id, cell_id);
        lyd_new_path(tree, ctx, path, dl_earfcn, 0, NULL);
    }
    if (pci) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:EUtranCell[id='%s']/_3gpp-common-managed-element:pci",
                 sn_id, me_id, fn_id, cell_id);
        lyd_new_path(tree, ctx, path, pci, 0, NULL);
    }

cleanup:
    free(sn_id);
    free(me_id);
    free(fn_id);
    free(cell_id);
    free(mcc);
    free(mnc);
    free(n_prb);
    free(dl_earfcn);
    free(pci);
    free(enb_serial);
    free(tx_gain);

    if (!tree) {
        return NULL;
    }
    if (lyd_validate_all(&tree, ctx, LYD_VALIDATE_PRESENT, NULL)) {
        lyd_free_siblings(tree);
        return NULL;
    }
    return tree;
}

static struct nc_server_reply *handle_get_config_rpc(struct lyd_node *rpc, struct nc_session *session) {
    const struct ly_ctx *ctx = nc_session_get_ctx(session);
    if (!g_control || !g_control[0]) {
        return rpc_error_msg(ctx, NC_ERR_OP_NOT_SUPPORTED, "get-config is disabled (control endpoint not configured).");
    }

    const struct lyd_node *source = find_descendant(rpc, "source");
    const struct lyd_node *ds_running = source ? child_by_name(source, "running") : NULL;
    const struct lyd_node *ds_candidate = source ? child_by_name(source, "candidate") : NULL;
    const char *ds = ds_candidate ? "candidate" : "running";
    if (!ds_running && !ds_candidate && source) {
        return rpc_error_msg(ctx, NC_ERR_INVALID_VALUE, "Only source running/candidate is supported.");
    }

    char url[512];
    snprintf(url, sizeof(url), "%s/v1/control/config/%s", g_control, ds);
    long code = 0;
    char *resp = NULL;
    int rc = http_get_json(url, &code, &resp);
    if (rc != 0) {
        free(resp);
        return rpc_error_msg(ctx, NC_ERR_OP_FAILED, "Failed to call internal get-config endpoint.");
    }
    if (code < 200 || code >= 300 || !resp || !strstr(resp, "\"status\":\"ok\"")) {
        char *msg = extract_message(resp);
        if (!msg) {
            msg = strdup("get-config failed");
        }
        struct nc_server_reply *r = rpc_error_msg(ctx, NC_ERR_OP_FAILED, msg ? msg : "get-config failed");
        free(msg);
        free(resp);
        return r;
    }

    char *raw = read_file(g_snapshot);
    struct lyd_node *snapshot_tree = load_datastore_tree(ctx, raw);
    struct lyd_node *cfg_tree = build_config_tree(ctx, snapshot_tree, resp);
    lyd_free_siblings(snapshot_tree);
    free(raw);
    free(resp);

    struct lyd_node *reply = NULL;
    if (lyd_dup_single(rpc, NULL, 0, &reply)) {
        lyd_free_siblings(cfg_tree);
        return nc_server_reply_ok();
    }

    struct lyd_node *filter = NULL;
    LY_ERR ret = lyd_find_path(rpc, "filter", 0, &filter);
    if (ret && (ret != LY_ENOTFOUND)) {
        lyd_free_siblings(reply);
        lyd_free_siblings(cfg_tree);
        return nc_server_reply_ok();
    }

    struct lyd_node *out = cfg_tree;
    if (filter && cfg_tree) {
        const char *type = meta_value_by_name(filter, "type");
        if (type && !strcmp(type, "xpath")) {
            const char *xpath = meta_value_by_name(filter, "select");
            struct lyd_node *f = filter_by_xpath(cfg_tree, xpath);
            if (f) {
                out = f;
                cfg_tree = NULL;
            }
        } else {
            char *xml = NULL;
            if (!lyd_any_value_str(filter, &xml) && xml) {
                char *xpath = subtree_filter_to_xpath(ctx, xml);
                if (xpath) {
                    struct lyd_node *f = filter_by_xpath(cfg_tree, xpath);
                    if (f) {
                        out = f;
                        cfg_tree = NULL;
                    }
                }
                free(xpath);
            }
            free(xml);
        }
    }

    if (out) {
        char *json = NULL;
        if (!lyd_print_mem(&json, out, LYD_JSON, LYD_PRINT_SHRINK)) {
            emit_netconf_get(session, json);
        }
        free(json);
    }

    if (out) {
        if (lyd_new_any(reply, NULL, "data", out, LYD_ANYDATA_DATATREE,
                        LYD_NEW_ANY_USE_VALUE | LYD_NEW_VAL_OUTPUT, NULL)) {
            lyd_free_siblings(reply);
            lyd_free_siblings(out);
            lyd_free_siblings(cfg_tree);
            return nc_server_reply_ok();
        }
        /* Ownership of the datatree (out) is transferred into reply->data. */
        out = NULL;
        cfg_tree = NULL;
    } else {
        if (lyd_new_any(reply, NULL, "data", "", LYD_ANYDATA_STRING, LYD_NEW_VAL_OUTPUT, NULL)) {
            lyd_free_siblings(reply);
            lyd_free_siblings(cfg_tree);
            return nc_server_reply_ok();
        }
    }

    struct nc_server_reply *rpl = nc_server_reply_data(reply, NC_WD_UNKNOWN, NC_PARAMTYPE_FREE);
    if (!rpl) {
        lyd_free_siblings(cfg_tree);
        return nc_server_reply_ok();
    }

    return rpl;
}

static struct nc_server_reply *rpc_cb(struct lyd_node *rpc, struct nc_session *session) {
    const char *rpc_name = LYD_NAME(rpc);
    const char *rpc_mod = lyd_owner_module(rpc)->name;

    if (!strcmp(rpc_name, "close-session") && !strcmp(rpc_mod, "ietf-netconf")) {
        return nc_clb_default_close_session(rpc, session);
    }
    if (!strcmp(rpc_name, "get-schema") && !strcmp(rpc_mod, "ietf-netconf-monitoring")) {
        return nc_clb_default_get_schema(rpc, session);
    }

    if (!strcmp(rpc_name, "edit-config") && !strcmp(rpc_mod, "ietf-netconf")) {
        return handle_edit_config_rpc(nc_session_get_ctx(session), rpc);
    }

    if (!strcmp(rpc_name, "commit") && !strcmp(rpc_mod, "ietf-netconf")) {
        return handle_commit_rpc(nc_session_get_ctx(session));
    }

    if (!strcmp(rpc_name, "get-config") && !strcmp(rpc_mod, "ietf-netconf")) {
        return handle_get_config_rpc(rpc, session);
    }

    if (!strcmp(rpc_name, "get") && !strcmp(rpc_mod, "ietf-netconf")) {
        const struct ly_ctx *ctx = nc_session_get_ctx(session);
        char *raw = read_file(g_snapshot);
        struct lyd_node *data = load_datastore_tree(ctx, raw);

        struct lyd_node *reply = NULL;
        if (lyd_dup_single(rpc, NULL, 0, &reply)) {
            lyd_free_siblings(data);
            free(raw);
            return nc_server_reply_ok();
        }

        /* Keep legacy NETCONF_GET log contract: always emit legacy payload when present. */
        if (data) {
            struct lyd_node *legacy = NULL;
            if (!lyd_find_path(data, "/ems-enb-metrics:enb_metrics", 0, &legacy) && legacy) {
                char *json = NULL;
                if (!lyd_print_mem(&json, legacy, LYD_JSON, LYD_PRINT_SHRINK)) {
                    emit_netconf_get(session, json);
                }
                free(json);
            }
        } else if (raw) {
            /* Fallback: snapshot file contents are valid JSON and still useful for request tracing. */
            emit_netconf_get(session, raw);
        }

        struct lyd_node *out = NULL;

        struct lyd_node *filter = NULL;
        LY_ERR ret = lyd_find_path(rpc, "filter", 0, &filter);
        if (ret && (ret != LY_ENOTFOUND)) {
            lyd_free_siblings(reply);
            lyd_free_siblings(data);
            free(raw);
            return nc_server_reply_ok();
        }

        if (!data) {
            free(raw);
            if (lyd_new_any(reply, NULL, "data", "", LYD_ANYDATA_STRING, LYD_NEW_VAL_OUTPUT, NULL)) {
                lyd_free_siblings(reply);
                return nc_server_reply_ok();
            }
            struct nc_server_reply *rpl = nc_server_reply_data(reply, NC_WD_UNKNOWN, NC_PARAMTYPE_FREE);
            if (!rpl) {
                return nc_server_reply_ok();
            }
            return rpl;
        }

        if (!filter) {
            /* Backward-compat default: return legacy ems-enb-metrics container only. */
            struct lyd_node *legacy = NULL;
            if (!lyd_find_path(data, "/ems-enb-metrics:enb_metrics", 0, &legacy) && legacy) {
                if (lyd_dup_single(legacy, NULL, LYD_DUP_RECURSIVE, &out)) {
                    out = NULL;
                }
            }
        } else {
            const char *type = meta_value_by_name(filter, "type");
            if (type && !strcmp(type, "xpath")) {
                const char *xpath = meta_value_by_name(filter, "select");
                out = filter_by_xpath(data, xpath);
            } else {
                char *xml = NULL;
                if (!lyd_any_value_str(filter, &xml) && xml) {
                    char *xpath = subtree_filter_to_xpath(ctx, xml);
                    if (xpath) {
                        out = filter_by_xpath(data, xpath);
                    }
                    free(xpath);
                }
                free(xml);
            }
        }

        if (!out) {
            /* Fallback to full data if filtering yields nothing. */
            out = data;
            data = NULL;
        } else {
            lyd_free_siblings(data);
            data = NULL;
        }

        if (lyd_new_any(reply, NULL, "data", out, LYD_ANYDATA_DATATREE,
                        LYD_NEW_ANY_USE_VALUE | LYD_NEW_VAL_OUTPUT, NULL)) {
            lyd_free_siblings(reply);
            lyd_free_siblings(out);
            free(raw);
            return nc_server_reply_ok();
        }

        struct nc_server_reply *rpl = nc_server_reply_data(reply, NC_WD_UNKNOWN, NC_PARAMTYPE_FREE);
        if (!rpl) {
            free(raw);
            return nc_server_reply_ok();
        }

        free(raw);
        return rpl;
    }

    return nc_server_reply_ok();
}

static void usage(const char *prog) {
    fprintf(stderr,
            "Usage: %s -addr <host:port> -yang <dir> -snapshot <path> -hostkey <path> -authorized-key <path> -user <name[,name...]> [-control <url>]\n",
            prog);
}

int main(int argc, char **argv) {
    const char *addr = NULL;
    const char *yang_dir = NULL;
    const char *hostkey = NULL;
    const char *auth_key = NULL;
    const char *user = "admin";

    for (int i = 1; i < argc; ++i) {
        if (!strcmp(argv[i], "-addr") && i + 1 < argc) {
            addr = argv[++i];
        } else if (!strcmp(argv[i], "-yang") && i + 1 < argc) {
            yang_dir = argv[++i];
        } else if (!strcmp(argv[i], "-snapshot") && i + 1 < argc) {
            g_snapshot = argv[++i];
        } else if (!strcmp(argv[i], "-hostkey") && i + 1 < argc) {
            hostkey = argv[++i];
        } else if (!strcmp(argv[i], "-authorized-key") && i + 1 < argc) {
            auth_key = argv[++i];
        } else if (!strcmp(argv[i], "-user") && i + 1 < argc) {
            user = argv[++i];
        } else if (!strcmp(argv[i], "-control") && i + 1 < argc) {
            g_control = argv[++i];
        } else if (!strcmp(argv[i], "-h") || !strcmp(argv[i], "--help")) {
            usage(argv[0]);
            return 0;
        }
    }

    if (!addr || !yang_dir || !g_snapshot || !hostkey || !auth_key) {
        usage(argv[0]);
        return 1;
    }

    char *host = NULL;
    char *port_str = NULL;
    char *addr_copy = strdup(addr);
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
    port_str++;
    uint16_t port = (uint16_t)atoi(port_str);

    signal(SIGINT, on_sigint);
    signal(SIGTERM, on_sigint);

    curl_global_init(CURL_GLOBAL_DEFAULT);

    if (nc_server_init()) {
        free(addr_copy);
        return 1;
    }

    struct ly_ctx *ctx = NULL;
    if (nc_server_init_ctx(&ctx)) {
        free(addr_copy);
        nc_server_destroy();
        return 1;
    }

    if (nc_server_config_load_modules(&ctx)) {
        free(addr_copy);
        nc_server_destroy();
        ly_ctx_destroy(ctx);
        return 1;
    }

    const char *netconf_features[] = {"candidate", "xpath", NULL};
    (void)ly_ctx_load_module(ctx, "ietf-netconf", NULL, netconf_features);

    if (ly_ctx_set_searchdir(ctx, yang_dir)) {
        free(addr_copy);
        nc_server_destroy();
        ly_ctx_destroy(ctx);
        return 1;
    }

    if (!ly_ctx_load_module(ctx, g_mod_legacy, NULL, NULL) ||
        !ly_ctx_load_module(ctx, g_mod_common, NULL, NULL) ||
        !ly_ctx_load_module(ctx, g_mod_vendor, NULL, NULL)) {
        free(addr_copy);
        nc_server_destroy();
        ly_ctx_destroy(ctx);
        return 1;
    }

    struct lyd_node *config = NULL;
    if (nc_server_config_add_address_port(ctx, "ems-ssh", NC_TI_SSH, host, port, &config)) {
        free(addr_copy);
        nc_server_destroy();
        ly_ctx_destroy(ctx);
        return 1;
    }

    if (nc_server_config_add_ssh_hostkey(ctx, "ems-ssh", "hostkey", hostkey, NULL, &config)) {
        free(addr_copy);
        nc_server_destroy();
        ly_ctx_destroy(ctx);
        return 1;
    }

    char *users_copy = strdup(user);
    if (!users_copy) {
        free(addr_copy);
        nc_server_destroy();
        ly_ctx_destroy(ctx);
        return 1;
    }
    int added_user = 0;
    for (char *tok = strtok(users_copy, ","); tok; tok = strtok(NULL, ",")) {
        while (*tok == ' ') {
            tok++;
        }
        if (*tok == '\0') {
            continue;
        }
        if (nc_server_config_add_ssh_user_pubkey(ctx, "ems-ssh", tok, "nms-key", auth_key, &config)) {
            free(users_copy);
            free(addr_copy);
            nc_server_destroy();
            ly_ctx_destroy(ctx);
            return 1;
        }
        added_user = 1;
    }
    free(users_copy);
    if (!added_user) {
        free(addr_copy);
        nc_server_destroy();
        ly_ctx_destroy(ctx);
        return 1;
    }

    if (nc_server_config_setup_data(config)) {
        free(addr_copy);
        lyd_free_all(config);
        nc_server_destroy();
        ly_ctx_destroy(ctx);
        return 1;
    }

    lyd_free_all(config);

    nc_set_global_rpc_clb(rpc_cb);

    struct nc_pollsession *ps = nc_ps_new();
    if (!ps) {
        free(addr_copy);
        nc_server_destroy();
        ly_ctx_destroy(ctx);
        return 1;
    }

    while (!g_stop) {
        struct nc_session *session = NULL;
        int acc = nc_accept(100, ctx, &session);
        if (acc == NC_MSG_HELLO) {
            if (nc_ps_add_session(ps, session)) {
                nc_session_free(session, NULL);
            }
        }

        int ret = nc_ps_poll(ps, 100, &session);
        if (ret & NC_PSPOLL_SESSION_TERM) {
            if (session) {
                nc_ps_del_session(ps, session);
                nc_session_free(session, NULL);
            }
        }
    }

    nc_ps_clear(ps, 1, NULL);
    nc_ps_free(ps);
    nc_server_destroy();
    ly_ctx_destroy(ctx);
    free(addr_copy);
    curl_global_cleanup();

    return 0;
}
