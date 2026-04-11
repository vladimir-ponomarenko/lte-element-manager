#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <time.h>

#include <openssl/sha.h>

#include <libyang/libyang.h>
#include <nc_server.h>

static volatile sig_atomic_t g_stop = 0;
static const char *g_snapshot = NULL;

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

static struct nc_server_reply *rpc_cb(struct lyd_node *rpc, struct nc_session *session) {
    const char *rpc_name = LYD_NAME(rpc);
    const char *rpc_mod = lyd_owner_module(rpc)->name;

    if (!strcmp(rpc_name, "close-session") && !strcmp(rpc_mod, "ietf-netconf")) {
        return nc_clb_default_close_session(rpc, session);
    }
    if (!strcmp(rpc_name, "get-schema") && !strcmp(rpc_mod, "ietf-netconf-monitoring")) {
        return nc_clb_default_get_schema(rpc, session);
    }

    if ((!strcmp(rpc_name, "get") || !strcmp(rpc_name, "get-config")) &&
        !strcmp(rpc_mod, "ietf-netconf")) {
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
            "Usage: %s -addr <host:port> -yang <dir> -snapshot <path> -hostkey <path> -authorized-key <path> -user <name[,name...]>\n",
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

    return 0;
}
