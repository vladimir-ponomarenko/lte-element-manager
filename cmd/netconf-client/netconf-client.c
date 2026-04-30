/**
 * Test NETCONF client for srsRAN_4G-nms.
 */

#include <getopt.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include <nc_client.h>
#include <nc_server.h>

static void
usage(FILE *out)
{
    fprintf(out,
            "Usage:\n"
            "  netconf-client [options] <rpc> [args...]\n"
            "\n"
            "Options:\n"
            "  -h, --help                 Show this help.\n"
            "  -H, --host <host>          SSH host (default: 127.0.0.1).\n"
            "  -p, --port <port>          SSH port.\n"
            "  -u, --ssh-user <user>      SSH username (default: admin).\n"
            "  -P, --ssh-pubkey <path>    SSH public key path.\n"
            "  -i, --ssh-privkey <path>   SSH private key path.\n"
            "  -t, --timeout-ms <ms>      RPC reply timeout in milliseconds (default: env NETCONF_RPC_TIMEOUT_MS or 5000).\n"
            "  -d, --debug                Enable debug logs.\n"
            "      --schema-dir <path>    libyang schema search path (default: /app/yang:/usr/local/share/yang/modules/libnetconf2:/usr/local/share/yang/modules/libyang).\n"
            "\n"
            "RPCs:\n"
            "  get [xpath-filter]\n"
            "  get-config [running|candidate|startup] [xpath-filter]\n"
            "  edit-config <running|candidate|startup> <xml-file>\n"
            "  validate [running|candidate|startup]\n"
            "  validate-config <xml-file>\n"
            "  commit\n"
            "  discard-changes\n"
            "  lock <running|candidate>\n"
            "  unlock <running|candidate>\n"
            "  subscribe [duration-sec]\n"
            "  sequence <xml-file|-> [commit|discard|no-commit]\n");
}

static NC_DATASTORE
string2datastore(const char *str)
{
    if (!str) {
        return 0;
    }
    if (!strcmp(str, "candidate")) {
        return NC_DATASTORE_CANDIDATE;
    } else if (!strcmp(str, "running")) {
        return NC_DATASTORE_RUNNING;
    } else if (!strcmp(str, "startup")) {
        return NC_DATASTORE_STARTUP;
    }
    return 0;
}

static char *
read_file(const char *path)
{
    FILE *f = fopen(path, "rb");
    long len;
    char *buf;
    size_t nread;

    if (!f) {
        fprintf(stderr, "Failed to open file: %s\n", path);
        return NULL;
    }
    if (fseek(f, 0, SEEK_END) != 0) {
        fclose(f);
        fprintf(stderr, "Failed to seek file: %s\n", path);
        return NULL;
    }
    len = ftell(f);
    if (len < 0) {
        fclose(f);
        fprintf(stderr, "Failed to stat file: %s\n", path);
        return NULL;
    }
    if (fseek(f, 0, SEEK_SET) != 0) {
        fclose(f);
        fprintf(stderr, "Failed to rewind file: %s\n", path);
        return NULL;
    }

    buf = (char *)malloc((size_t)len + 1);
    if (!buf) {
        fclose(f);
        fprintf(stderr, "Out of memory\n");
        return NULL;
    }

    nread = fread(buf, 1, (size_t)len, f);
    fclose(f);
    if (nread != (size_t)len) {
        free(buf);
        fprintf(stderr, "Failed to read file: %s\n", path);
        return NULL;
    }
    buf[len] = '\0';
    return buf;
}

static char *
read_stdin_all(void)
{
    size_t cap = 4096;
    size_t len = 0;
    char *buf = (char *)malloc(cap);
    if (!buf) {
        fprintf(stderr, "Out of memory\n");
        return NULL;
    }

    while (!feof(stdin)) {
        if (len + 4096 > cap) {
            cap *= 2;
            char *nb = (char *)realloc(buf, cap);
            if (!nb) {
                free(buf);
                fprintf(stderr, "Out of memory\n");
                return NULL;
            }
            buf = nb;
        }
        size_t n = fread(buf + len, 1, cap - len - 1, stdin);
        len += n;
        if (ferror(stdin)) {
            free(buf);
            fprintf(stderr, "Failed to read stdin\n");
            return NULL;
        }
    }
    buf[len] = '\0';
    return buf;
}

static int
tree_has_rpc_error(const struct lyd_node *node)
{
    const struct lyd_node *it = NULL;

    LY_LIST_FOR(node, it) {
        if (!strcmp(LYD_NAME(it), "rpc-error")) {
            return 1;
        }
        if (lyd_child(it) && tree_has_rpc_error(lyd_child(it))) {
            return 1;
        }
    }
    return 0;
}

static int
send_and_print(struct nc_session *session, struct nc_rpc *rpc, int timeout_ms)
{
    int r;
    int rc = 0;
    uint64_t msg_id = 0;
    struct lyd_node *envp = NULL, *op = NULL;

    r = nc_send_rpc(session, rpc, 1000, &msg_id);
    if (r != NC_MSG_RPC) {
        fprintf(stderr, "Failed to send RPC\n");
        rc = 1;
        goto cleanup;
    }

    r = nc_recv_reply(session, rpc, msg_id, timeout_ms, &envp, &op);
    if ((r != NC_MSG_REPLY) && (r != NC_MSG_REPLY_ERR_MSGID)) {
        if (r == NC_MSG_WOULDBLOCK) {
            fprintf(stderr, "Timed out waiting for reply (%d ms)\n", timeout_ms);
        } else {
            fprintf(stderr, "Failed to receive reply\n");
        }
        rc = 1;
        goto cleanup;
    }

    if (op) {
        if (lyd_print_file(stdout, op, LYD_XML, 0)) {
            fprintf(stderr, "Failed to print reply data\n");
            rc = 1;
            goto cleanup;
        }
    }
    if (lyd_print_file(stdout, envp, LYD_XML, 0)) {
        fprintf(stderr, "Failed to print reply envelope\n");
        rc = 1;
        goto cleanup;
    }
    if (tree_has_rpc_error(envp) || tree_has_rpc_error(op)) {
        rc = 1;
    }

cleanup:
    lyd_free_all(envp);
    lyd_free_all(op);
    return rc;
}

static int
send_named_rpc(struct nc_session *session, struct nc_rpc *rpc, int timeout_ms, const char *name)
{
    int rc;

    if (!rpc) {
        fprintf(stderr, "%s: failed to create RPC\n", name ? name : "rpc");
        return 1;
    }
    if (name && name[0]) {
        printf("[netconf-client] %s\n", name);
    }
    rc = send_and_print(session, rpc, timeout_ms);
    nc_rpc_free(rpc);
    return rc;
}

static int
run_sequence(struct nc_session *session, const char *xml_path, const char *action, int timeout_ms)
{
    int rc = 0;
    int first_rc = 0;
    int locked_candidate = 0;
    int locked_running = 0;
    char *edit_content = NULL;
    struct nc_rpc *rpc = NULL;

    if (!action || !action[0]) {
        action = "commit";
    }
    if (strcmp(action, "commit") && strcmp(action, "discard") && strcmp(action, "no-commit")) {
        fprintf(stderr, "sequence action must be one of: commit, discard, no-commit\n");
        return 2;
    }

    if (!strcmp(xml_path, "-")) {
        edit_content = read_stdin_all();
    } else {
        edit_content = read_file(xml_path);
    }
    if (!edit_content) {
        return 1;
    }

    rc = send_named_rpc(session, nc_rpc_lock(NC_DATASTORE_CANDIDATE), timeout_ms, "lock candidate");
    if (rc) {
        first_rc = rc;
        goto cleanup;
    }
    locked_candidate = 1;

    if (!strcmp(action, "commit")) {
        rc = send_named_rpc(session, nc_rpc_lock(NC_DATASTORE_RUNNING), timeout_ms, "lock running");
        if (rc && !first_rc) {
            first_rc = rc;
        }
        if (rc) {
            goto cleanup;
        }
        locked_running = 1;
    }

    rpc = nc_rpc_edit(NC_DATASTORE_CANDIDATE, NC_RPC_EDIT_DFLTOP_UNKNOWN, NC_RPC_EDIT_TESTOPT_UNKNOWN,
            NC_RPC_EDIT_ERROPT_UNKNOWN, edit_content, NC_PARAMTYPE_CONST);
    rc = send_named_rpc(session, rpc, timeout_ms, "edit-config candidate");
    rpc = NULL;
    if (rc && !first_rc) {
        first_rc = rc;
    }
    if (rc) {
        goto cleanup;
    }

    rc = send_named_rpc(session, nc_rpc_validate(NC_DATASTORE_CANDIDATE, NULL, NC_PARAMTYPE_CONST), timeout_ms,
            "validate candidate");
    if (rc && !first_rc) {
        first_rc = rc;
    }
    if (rc) {
        goto cleanup;
    }

    if (!strcmp(action, "commit")) {
        rc = send_named_rpc(session, nc_rpc_commit(0, 0, NULL, NULL, NC_PARAMTYPE_CONST), timeout_ms, "commit");
    } else if (!strcmp(action, "discard")) {
        rc = send_named_rpc(session, nc_rpc_discard(), timeout_ms, "discard-changes");
    } else {
        rc = 0;
    }
    if (rc && !first_rc) {
        first_rc = rc;
    }

cleanup:
    if (locked_running) {
        rc = send_named_rpc(session, nc_rpc_unlock(NC_DATASTORE_RUNNING), timeout_ms, "unlock running");
        if (rc && !first_rc) {
            first_rc = rc;
        }
    }
    if (locked_candidate) {
        rc = send_named_rpc(session, nc_rpc_unlock(NC_DATASTORE_CANDIDATE), timeout_ms, "unlock candidate");
        if (rc && !first_rc) {
            first_rc = rc;
        }
    }
    free(edit_content);
    return first_rc;
}

static int
run_subscribe(struct nc_session *session, int duration_sec, int timeout_ms)
{
    int rc;
    time_t end = 0;

    if (duration_sec > 0) {
        end = time(NULL) + duration_sec;
    }

    rc = send_named_rpc(session, nc_rpc_subscribe(NULL, NULL, NULL, NULL, NC_PARAMTYPE_CONST), timeout_ms,
            "create-subscription");
    if (rc) {
        return rc;
    }

    while (duration_sec <= 0 || time(NULL) < end) {
        struct lyd_node *envp = NULL, *op = NULL;
        NC_MSG_TYPE msg = nc_recv_notif(session, 1000, &envp, &op);
        if (msg == NC_MSG_WOULDBLOCK) {
            continue;
        }
        if (msg == NC_MSG_ERROR) {
            fprintf(stderr, "Failed to receive notification\n");
            lyd_free_all(envp);
            lyd_free_all(op);
            return 1;
        }
        if (msg == NC_MSG_NOTIF) {
            if (op && lyd_print_file(stdout, op, LYD_XML, 0)) {
                fprintf(stderr, "Failed to print notification data\n");
                lyd_free_all(envp);
                lyd_free_all(op);
                return 1;
            }
            if (envp && lyd_print_file(stdout, envp, LYD_XML, 0)) {
                fprintf(stderr, "Failed to print notification envelope\n");
                lyd_free_all(envp);
                lyd_free_all(op);
                return 1;
            }
            fflush(stdout);
        }
        lyd_free_all(envp);
        lyd_free_all(op);
    }
    return 0;
}

static const char *
pick_first_existing_dir(const char *pathlist)
{
    static char buf[1024];
    const char *p, *start;

    if (!pathlist || !pathlist[0]) {
        return NULL;
    }
    if (!strchr(pathlist, ':')) {
        return pathlist;
    }

    start = pathlist;
    while (*start) {
        p = strchr(start, ':');
        size_t len = p ? (size_t)(p - start) : strlen(start);
        if (len > 0 && len < sizeof(buf)) {
            memcpy(buf, start, len);
            buf[len] = '\0';
            if (!access(buf, F_OK)) {
                return buf;
            }
        }
        if (!p) {
            break;
        }
        start = p + 1;
    }

    /* Fall back to the whole string. */
    return pathlist;
}

int
main(int argc, char **argv)
{
    int rc = 0;
    int opt;
    int port = 0;
    int timeout_ms = 5000;
    const char *host = "127.0.0.1";
    const char *user = "admin";
    const char *ssh_pubkey_path = NULL;
    const char *ssh_privkey_path = NULL;
    const char *schema_dir = NULL;
    struct nc_session *session = NULL;

    enum {
        OPT_SCHEMA_DIR = 1000,
    };

    struct option options[] = {
        {"help", no_argument, NULL, 'h'},
        {"host", required_argument, NULL, 'H'},
        {"port", required_argument, NULL, 'p'},
        {"ssh-user", required_argument, NULL, 'u'},
        {"ssh-pubkey", required_argument, NULL, 'P'},
        {"ssh-privkey", required_argument, NULL, 'i'},
        {"timeout-ms", required_argument, NULL, 't'},
        {"debug", no_argument, NULL, 'd'},
        {"schema-dir", required_argument, NULL, OPT_SCHEMA_DIR},
        {NULL, 0, NULL, 0}
    };

    if (argc == 1) {
        usage(stdout);
        return 1;
    }

    const char *env_timeout = getenv("NETCONF_RPC_TIMEOUT_MS");
    if (env_timeout && env_timeout[0]) {
        timeout_ms = (int)strtoul(env_timeout, NULL, 10);
    }

    while ((opt = getopt_long(argc, argv, "hH:p:u:P:i:t:d", options, NULL)) != -1) {
        switch (opt) {
        case 'h':
            usage(stdout);
            return 0;
        case 'H':
            host = optarg;
            break;
        case 'p':
            port = (int)strtoul(optarg, NULL, 10);
            break;
        case 'u':
            user = optarg;
            break;
        case 'P':
            ssh_pubkey_path = optarg;
            break;
        case 'i':
            ssh_privkey_path = optarg;
            break;
        case 't':
            timeout_ms = (int)strtoul(optarg, NULL, 10);
            break;
        case 'd':
            nc_verbosity(NC_VERB_DEBUG);
            nc_libssh_thread_verbosity(2);
            break;
        case OPT_SCHEMA_DIR:
            schema_dir = optarg;
            break;
        default:
            fprintf(stderr, "Invalid option or missing argument\n");
            return 2;
        }
    }

    if (!port) {
        fprintf(stderr, "Missing required --port\n");
        return 2;
    }
    if (optind >= argc) {
        fprintf(stderr, "Missing RPC name\n");
        return 2;
    }

    if (!schema_dir) {
        schema_dir = getenv("NETCONF_SCHEMA_DIR");
    }
    if (!schema_dir) {
        schema_dir = "/app/yang";
    }
    schema_dir = pick_first_existing_dir(schema_dir);
    nc_client_set_schema_searchpath(schema_dir);

    /* known_hosts support (non-interactive by default in containers) */
    const char *kh_path = getenv("NETCONF_KNOWN_HOSTS");
    const char *kh_mode = getenv("NETCONF_KNOWN_HOSTS_MODE");
    if (kh_path && kh_path[0]) {
        if (nc_client_ssh_set_knownhosts_path(kh_path)) {
            fprintf(stderr, "Failed to set known_hosts path: %s\n", kh_path);
            rc = 1;
            goto cleanup;
        }
    }
    if (kh_mode && kh_mode[0]) {
        NC_SSH_KNOWNHOSTS_MODE mode = NC_SSH_KNOWNHOSTS_ASK;
        if (!strcmp(kh_mode, "accept")) {
            mode = NC_SSH_KNOWNHOSTS_ACCEPT;
        } else if (!strcmp(kh_mode, "strict")) {
            mode = NC_SSH_KNOWNHOSTS_STRICT;
        } else if (!strcmp(kh_mode, "ask")) {
            mode = NC_SSH_KNOWNHOSTS_ASK;
        }
        nc_client_ssh_set_knownhosts_mode(mode);
    }

    if (nc_client_ssh_set_username(user)) {
        fprintf(stderr, "Failed to set SSH username\n");
        rc = 1;
        goto cleanup;
    }
    if (ssh_pubkey_path && ssh_privkey_path) {
        if (nc_client_ssh_add_keypair(ssh_pubkey_path, ssh_privkey_path)) {
            fprintf(stderr, "Couldn't set client's SSH keypair.\n");
            rc = 1;
            goto cleanup;
        }
    } else {
        fprintf(stderr, "Both --ssh-pubkey and --ssh-privkey are required\n");
        rc = 2;
        goto cleanup;
    }

    session = nc_connect_ssh(host, port, NULL);
    if (!session) {
        fprintf(stderr, "Couldn't connect to NETCONF server at %s:%d\n", host, port);
        rc = 1;
        goto cleanup;
    }
    {
        struct ly_ctx *ctx = (struct ly_ctx *)nc_session_get_ctx(session);
        if (ctx) {
            (void)ly_ctx_load_module(ctx, "notifications", NULL, NULL);
            (void)ly_ctx_load_module(ctx, "ems-fault-management", NULL, NULL);
        }
    }

    const char *rpc_name = argv[optind];
    struct nc_rpc *rpc = NULL;
    char *edit_content = NULL;
    int edit_owned_by_rpc = 0;

    if (!strcmp(rpc_name, "get")) {
        const char *filter = NULL;
        if (optind + 1 < argc) {
            filter = argv[optind + 1];
        }
        rpc = nc_rpc_get(filter, NC_WD_UNKNOWN, NC_PARAMTYPE_CONST);
    } else if (!strcmp(rpc_name, "get-config")) {
        NC_DATASTORE ds = NC_DATASTORE_RUNNING;
        const char *filter = NULL;
        if (optind + 1 < argc) {
            NC_DATASTORE parsed = string2datastore(argv[optind + 1]);
            if (parsed) {
                ds = parsed;
                if (optind + 2 < argc) {
                    filter = argv[optind + 2];
                }
            } else {
                filter = argv[optind + 1];
            }
        }
        rpc = nc_rpc_getconfig(ds, filter, NC_WD_UNKNOWN, NC_PARAMTYPE_CONST);
    } else if (!strcmp(rpc_name, "edit-config")) {
        if (optind + 2 >= argc) {
            fprintf(stderr, "edit-config requires: <datastore> <xml-file>\n");
            rc = 2;
            goto cleanup;
        }
        NC_DATASTORE ds = string2datastore(argv[optind + 1]);
        if (!ds) {
            fprintf(stderr, "Invalid datastore: %s\n", argv[optind + 1]);
            rc = 2;
            goto cleanup;
        }
        if (!strcmp(argv[optind + 2], "-")) {
            edit_content = read_stdin_all();
        } else {
            edit_content = read_file(argv[optind + 2]);
        }
        if (!edit_content) {
            rc = 1;
            goto cleanup;
        }
        /* Keep ownership local. */
        rpc = nc_rpc_edit(ds, NC_RPC_EDIT_DFLTOP_UNKNOWN, NC_RPC_EDIT_TESTOPT_UNKNOWN, NC_RPC_EDIT_ERROPT_UNKNOWN,
                edit_content, NC_PARAMTYPE_CONST);
    } else if (!strcmp(rpc_name, "validate")) {
        NC_DATASTORE ds = NC_DATASTORE_CANDIDATE;
        if (optind + 1 < argc) {
            ds = string2datastore(argv[optind + 1]);
            if (!ds) {
                fprintf(stderr, "Invalid datastore: %s\n", argv[optind + 1]);
                rc = 2;
                goto cleanup;
            }
        }
        rpc = nc_rpc_validate(ds, NULL, NC_PARAMTYPE_CONST);
    } else if (!strcmp(rpc_name, "validate-config")) {
        if (optind + 1 >= argc) {
            fprintf(stderr, "validate-config requires: <xml-file>\n");
            rc = 2;
            goto cleanup;
        }
        if (!strcmp(argv[optind + 1], "-")) {
            edit_content = read_stdin_all();
        } else {
            edit_content = read_file(argv[optind + 1]);
        }
        if (!edit_content) {
            rc = 1;
            goto cleanup;
        }
        rpc = nc_rpc_validate(NC_DATASTORE_CONFIG, edit_content, NC_PARAMTYPE_CONST);
    } else if (!strcmp(rpc_name, "commit")) {
        rpc = nc_rpc_commit(0, 0, NULL, NULL, NC_PARAMTYPE_CONST);
    } else if (!strcmp(rpc_name, "discard-changes")) {
        rpc = nc_rpc_discard();
    } else if (!strcmp(rpc_name, "lock")) {
        NC_DATASTORE ds;
        if (optind + 1 >= argc) {
            fprintf(stderr, "lock requires: <running|candidate>\n");
            rc = 2;
            goto cleanup;
        }
        ds = string2datastore(argv[optind + 1]);
        if (!ds || ds == NC_DATASTORE_STARTUP) {
            fprintf(stderr, "Invalid datastore for lock: %s\n", argv[optind + 1]);
            rc = 2;
            goto cleanup;
        }
        rpc = nc_rpc_lock(ds);
    } else if (!strcmp(rpc_name, "unlock")) {
        NC_DATASTORE ds;
        if (optind + 1 >= argc) {
            fprintf(stderr, "unlock requires: <running|candidate>\n");
            rc = 2;
            goto cleanup;
        }
        ds = string2datastore(argv[optind + 1]);
        if (!ds || ds == NC_DATASTORE_STARTUP) {
            fprintf(stderr, "Invalid datastore for unlock: %s\n", argv[optind + 1]);
            rc = 2;
            goto cleanup;
        }
        rpc = nc_rpc_unlock(ds);
    } else if (!strcmp(rpc_name, "subscribe")) {
        int duration_sec = 30;
        if (optind + 1 < argc) {
            duration_sec = (int)strtol(argv[optind + 1], NULL, 10);
        }
        rc = run_subscribe(session, duration_sec, timeout_ms);
        goto cleanup;
    } else if (!strcmp(rpc_name, "sequence")) {
        const char *action = "commit";
        if (optind + 1 >= argc) {
            fprintf(stderr, "sequence requires: <xml-file|-> [commit|discard|no-commit]\n");
            rc = 2;
            goto cleanup;
        }
        if (optind + 2 < argc) {
            action = argv[optind + 2];
        }
        rc = run_sequence(session, argv[optind + 1], action, timeout_ms);
        goto cleanup;
    } else {
        fprintf(stderr, "Unknown RPC: %s\n", rpc_name);
        rc = 2;
        goto cleanup;
    }

    if (!rpc) {
        fprintf(stderr, "Failed to create RPC\n");
        rc = 1;
        goto cleanup;
    }

    rc = send_and_print(session, rpc, timeout_ms);

cleanup:
    if (!edit_owned_by_rpc) {
        free(edit_content);
    }
    nc_rpc_free(rpc);
    nc_session_free(session, NULL);
    nc_client_destroy();
    return rc;
}
