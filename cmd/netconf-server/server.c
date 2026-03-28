#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <time.h>

#include <libyang/libyang.h>
#include <nc_server.h>

static volatile sig_atomic_t g_stop = 0;
static const char *g_snapshot = NULL;
static const char *g_module = "ems-enb-metrics";

static void emit_netconf_get(const struct nc_session *session, const char *raw) {
    if (!raw) {
        return;
    }
    char ts[32] = {0};
    time_t now = time(NULL);
    struct tm tm_now;
    localtime_r(&now, &tm_now);
    strftime(ts, sizeof(ts), "%Y-%m-%dT%H:%M:%S%z", &tm_now);
    const char *user = nc_session_get_username(session);
    fprintf(stdout, "NETCONF_GET user=%s ts=%s json=%s\n", user ? user : "unknown", ts, raw);
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

static struct lyd_node *load_metrics_tree(const struct ly_ctx *ctx, const char *raw) {
    if (!raw) {
        return NULL;
    }

    size_t wrap_len = strlen(raw) + strlen(g_module) + 32;
    char *wrapped = (char *)malloc(wrap_len);
    if (!wrapped) {
        return NULL;
    }

    snprintf(wrapped, wrap_len, "{\"%s:enb_metrics\":%s}", g_module, raw);

    struct lyd_node *tree = NULL;
    if (lyd_parse_data_mem(ctx, wrapped, LYD_JSON, LYD_PARSE_STRICT, LYD_VALIDATE_PRESENT, &tree)) {
        free(wrapped);
        return NULL;
    }

    free(wrapped);
    return tree;
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
        struct lyd_node *metrics = load_metrics_tree(ctx, raw);

        struct lyd_node *reply = NULL;
        if (lyd_dup_single(rpc, NULL, 0, &reply)) {
            lyd_free_siblings(metrics);
            free(raw);
            return nc_server_reply_ok();
        }

        if (!metrics) {
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

        if (lyd_new_any(reply, NULL, "data", metrics, LYD_ANYDATA_DATATREE,
                        LYD_NEW_ANY_USE_VALUE | LYD_NEW_VAL_OUTPUT, NULL)) {
            lyd_free_siblings(reply);
            lyd_free_siblings(metrics);
            free(raw);
            return nc_server_reply_ok();
        }

        struct nc_server_reply *rpl = nc_server_reply_data(reply, NC_WD_UNKNOWN, NC_PARAMTYPE_FREE);
        if (!rpl) {
            free(raw);
            return nc_server_reply_ok();
        }

        emit_netconf_get(session, raw);
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

    if (!ly_ctx_load_module(ctx, g_module, NULL, NULL)) {
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
