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
    char *enb_id;
    char *mcc;
    char *mnc;
    char *mme_addr;
    char *gtp_bind_addr;
    char *s1c_bind_addr;
    char *s1c_bind_port;
    char *n_prb;
    char *tm;
    char *tx_gain;
    char *rx_gain;
    char *time_adv_nsamples;
    char *device_name;
    char *device_args;
    char *dl_earfcn;
    char *pci;
    char *cell_id;
    char *tac;
    char *ho_active;
    char *a3_offset;
    char *time_to_trigger;
    char *hysteresis;

    // scheduler
    char *sched_policy;
    char *pdsch_max_mcs;
    char *pusch_max_mcs;
    char *target_bler;
    char *min_nof_ctrl_symbols;
    char *max_nof_ctrl_symbols;

    // sib
    char *q_rx_lev_min;
    char *cell_barred;
    char *num_ra_preambles;
    char *preamble_init_rx_target_pwr;
    char *pwr_ramping_step;
    char *reference_signal_power;
    char *p0_nominal_pusch;
    char *p0_nominal_pucch;
    char *alpha;
    char *default_paging_cycle;
    char *t300;
    char *t301;
    char *t310;
    char *n310;
    char *t311;

    // rb (first qci entry)
    char *qci;
    char *discard_timer;
    char *pdcp_sn_size;
    char *t_poll_retx;
    char *max_retx_thresh;
    char *t_reordering;
    char *priority;

    // expert
    char *pusch_max_its;
    char *nr_pusch_max_its;
    char *pusch_8bit_decoder;
    char *nof_phy_threads;
    char *metrics_period_secs;
    char *tx_amplitude;
    char *rrc_inactivity_timer;
    char *rlf_release_timer_ms;
    char *eea_pref_list;
    char *eia_pref_list;
    char *gtpu_tunnel_timeout;
    char *s1_setup_max_retries;
    char *s1_connect_timer;
    char *rx_gain_offset;
    char *use_cedron_f_est_alg;
    char *rlf_min_ul_snr_estim;
    char *max_mac_dl_kos;
    char *max_mac_ul_kos;
};

static void free_edit_values(struct edit_values *v) {
    if (!v) {
        return;
    }
    free(v->enb_serial);
    free(v->enb_id);
    free(v->mcc);
    free(v->mnc);
    free(v->mme_addr);
    free(v->gtp_bind_addr);
    free(v->s1c_bind_addr);
    free(v->s1c_bind_port);
    free(v->n_prb);
    free(v->tm);
    free(v->tx_gain);
    free(v->rx_gain);
    free(v->time_adv_nsamples);
    free(v->device_name);
    free(v->device_args);
    free(v->dl_earfcn);
    free(v->pci);
    free(v->cell_id);
    free(v->tac);
    free(v->ho_active);
    free(v->a3_offset);
    free(v->time_to_trigger);
    free(v->hysteresis);

    free(v->sched_policy);
    free(v->pdsch_max_mcs);
    free(v->pusch_max_mcs);
    free(v->target_bler);
    free(v->min_nof_ctrl_symbols);
    free(v->max_nof_ctrl_symbols);

    free(v->q_rx_lev_min);
    free(v->cell_barred);
    free(v->num_ra_preambles);
    free(v->preamble_init_rx_target_pwr);
    free(v->pwr_ramping_step);
    free(v->reference_signal_power);
    free(v->p0_nominal_pusch);
    free(v->p0_nominal_pucch);
    free(v->alpha);
    free(v->default_paging_cycle);
    free(v->t300);
    free(v->t301);
    free(v->t310);
    free(v->n310);
    free(v->t311);

    free(v->qci);
    free(v->discard_timer);
    free(v->pdcp_sn_size);
    free(v->t_poll_retx);
    free(v->max_retx_thresh);
    free(v->t_reordering);
    free(v->priority);

    free(v->pusch_max_its);
    free(v->nr_pusch_max_its);
    free(v->pusch_8bit_decoder);
    free(v->nof_phy_threads);
    free(v->metrics_period_secs);
    free(v->tx_amplitude);
    free(v->rrc_inactivity_timer);
    free(v->rlf_release_timer_ms);
    free(v->eea_pref_list);
    free(v->eia_pref_list);
    free(v->gtpu_tunnel_timeout);
    free(v->s1_setup_max_retries);
    free(v->s1_connect_timer);
    free(v->rx_gain_offset);
    free(v->use_cedron_f_est_alg);
    free(v->rlf_min_ul_snr_estim);
    free(v->max_mac_dl_kos);
    free(v->max_mac_ul_kos);
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
    return !strcmp(name, "enb_serial") || !strcmp(name, "enb_id") ||
           !strcmp(name, "mcc") || !strcmp(name, "mnc") || !strcmp(name, "mme_addr") ||
           !strcmp(name, "gtp_bind_addr") || !strcmp(name, "s1c_bind_addr") || !strcmp(name, "s1c_bind_port") ||
           !strcmp(name, "n_prb") || !strcmp(name, "tm") ||
           !strcmp(name, "tx_gain") || !strcmp(name, "rx_gain") || !strcmp(name, "time_adv_nsamples") ||
           !strcmp(name, "device_name") || !strcmp(name, "device_args") ||
           !strcmp(name, "cell_id") || !strcmp(name, "tac") || !strcmp(name, "dl_earfcn") || !strcmp(name, "pci") ||
           !strcmp(name, "ho_active") || !strcmp(name, "a3_offset") || !strcmp(name, "time_to_trigger") || !strcmp(name, "hysteresis") ||
           !strcmp(name, "sched_policy") || !strcmp(name, "pdsch_max_mcs") || !strcmp(name, "pusch_max_mcs") ||
           !strcmp(name, "target_bler") || !strcmp(name, "min_nof_ctrl_symbols") || !strcmp(name, "max_nof_ctrl_symbols") ||
           !strcmp(name, "q_rx_lev_min") || !strcmp(name, "cell_barred") || !strcmp(name, "num_ra_preambles") ||
           !strcmp(name, "preamble_init_rx_target_pwr") || !strcmp(name, "pwr_ramping_step") || !strcmp(name, "reference_signal_power") ||
           !strcmp(name, "p0_nominal_pusch") || !strcmp(name, "p0_nominal_pucch") || !strcmp(name, "alpha") ||
           !strcmp(name, "default_paging_cycle") || !strcmp(name, "t300") || !strcmp(name, "t301") || !strcmp(name, "t310") ||
           !strcmp(name, "n310") || !strcmp(name, "t311") ||
           !strcmp(name, "qci") || !strcmp(name, "discard_timer") || !strcmp(name, "pdcp_sn_size") || !strcmp(name, "t_poll_retx") ||
           !strcmp(name, "max_retx_thresh") || !strcmp(name, "t_reordering") || !strcmp(name, "priority") ||
           !strcmp(name, "pusch_max_its") || !strcmp(name, "nr_pusch_max_its") || !strcmp(name, "pusch_8bit_decoder") ||
           !strcmp(name, "nof_phy_threads") || !strcmp(name, "metrics_period_secs") ||
           !strcmp(name, "tx_amplitude") || !strcmp(name, "rrc_inactivity_timer") || !strcmp(name, "rlf_release_timer_ms") ||
           !strcmp(name, "eea_pref_list") || !strcmp(name, "eia_pref_list") || !strcmp(name, "gtpu_tunnel_timeout") ||
           !strcmp(name, "s1_setup_max_retries") || !strcmp(name, "s1_connect_timer") || !strcmp(name, "rx_gain_offset") ||
           !strcmp(name, "use_cedron_f_est_alg") ||
           !strcmp(name, "rlf_min_ul_snr_estim") || !strcmp(name, "max_mac_dl_kos") || !strcmp(name, "max_mac_ul_kos");
}

static int is_numeric_leaf(const char *name) {
    return !strcmp(name, "s1c_bind_port") || !strcmp(name, "n_prb") || !strcmp(name, "tm") ||
           !strcmp(name, "tx_gain") || !strcmp(name, "rx_gain") ||
           !strcmp(name, "dl_earfcn") || !strcmp(name, "pci") ||
           !strcmp(name, "a3_offset") || !strcmp(name, "time_to_trigger") || !strcmp(name, "hysteresis") ||
           !strcmp(name, "pdsch_max_mcs") || !strcmp(name, "pusch_max_mcs") ||
           !strcmp(name, "target_bler") || !strcmp(name, "min_nof_ctrl_symbols") || !strcmp(name, "max_nof_ctrl_symbols") ||
           !strcmp(name, "q_rx_lev_min") || !strcmp(name, "num_ra_preambles") || !strcmp(name, "preamble_init_rx_target_pwr") ||
           !strcmp(name, "pwr_ramping_step") || !strcmp(name, "reference_signal_power") || !strcmp(name, "p0_nominal_pusch") ||
           !strcmp(name, "p0_nominal_pucch") || !strcmp(name, "alpha") || !strcmp(name, "default_paging_cycle") ||
           !strcmp(name, "t300") || !strcmp(name, "t301") || !strcmp(name, "t310") || !strcmp(name, "n310") || !strcmp(name, "t311") ||
           !strcmp(name, "qci") || !strcmp(name, "discard_timer") || !strcmp(name, "pdcp_sn_size") || !strcmp(name, "t_poll_retx") ||
           !strcmp(name, "max_retx_thresh") || !strcmp(name, "t_reordering") || !strcmp(name, "priority") ||
           !strcmp(name, "pusch_max_its") || !strcmp(name, "nof_phy_threads") || !strcmp(name, "metrics_period_secs") ||
           !strcmp(name, "tx_amplitude") || !strcmp(name, "rrc_inactivity_timer") || !strcmp(name, "rlf_release_timer_ms") ||
           !strcmp(name, "gtpu_tunnel_timeout") || !strcmp(name, "s1_setup_max_retries") || !strcmp(name, "s1_connect_timer") ||
           !strcmp(name, "rx_gain_offset") || !strcmp(name, "rlf_min_ul_snr_estim") || !strcmp(name, "max_mac_dl_kos") || !strcmp(name, "max_mac_ul_kos");
}

static int is_boolean_leaf(const char *name) {
    return !strcmp(name, "ho_active") || !strcmp(name, "pusch_8bit_decoder") || !strcmp(name, "use_cedron_f_est_alg");
}

static const struct lyd_node *ancestor_by_name(const struct lyd_node *n, const char *name) {
    const struct lyd_node *p = n ? lyd_parent(n) : NULL;
    while (p) {
        if (!strcmp(LYD_NAME(p), name)) {
            return p;
        }
        p = lyd_parent(p);
    }
    return NULL;
}

static void collect_edit_values(const struct lyd_node *n, struct edit_values *vals) {
    if (!n || !vals) {
        return;
    }

    const struct lyd_node *it;
    LY_LIST_FOR(n, it) {
        const char *name = LYD_NAME(it);
        const char *val = lyd_get_value(it);
        if (ancestor_by_name(it, "qci_profiles")) {
            collect_edit_values(lyd_child(it), vals);
            continue;
        }
        if (name && val && is_supported_leaf(name)) {
            if (!strcmp(name, "enb_serial")) {
                set_str(&vals->enb_serial, val);
            } else if (!strcmp(name, "enb_id")) {
                set_str(&vals->enb_id, val);
            } else if (!strcmp(name, "mcc")) {
                set_str(&vals->mcc, val);
            } else if (!strcmp(name, "mnc")) {
                set_str(&vals->mnc, val);
            } else if (!strcmp(name, "mme_addr")) {
                set_str(&vals->mme_addr, val);
            } else if (!strcmp(name, "gtp_bind_addr")) {
                set_str(&vals->gtp_bind_addr, val);
            } else if (!strcmp(name, "s1c_bind_addr")) {
                set_str(&vals->s1c_bind_addr, val);
            } else if (!strcmp(name, "s1c_bind_port")) {
                set_str(&vals->s1c_bind_port, val);
            } else if (!strcmp(name, "n_prb")) {
                set_str(&vals->n_prb, val);
            } else if (!strcmp(name, "tm")) {
                set_str(&vals->tm, val);
            } else if (!strcmp(name, "tx_gain")) {
                set_str(&vals->tx_gain, val);
            } else if (!strcmp(name, "rx_gain")) {
                set_str(&vals->rx_gain, val);
            } else if (!strcmp(name, "time_adv_nsamples")) {
                set_str(&vals->time_adv_nsamples, val);
            } else if (!strcmp(name, "device_name")) {
                set_str(&vals->device_name, val);
            } else if (!strcmp(name, "device_args")) {
                set_str(&vals->device_args, val);
            } else if (!strcmp(name, "dl_earfcn")) {
                set_str(&vals->dl_earfcn, val);
            } else if (!strcmp(name, "pci")) {
                set_str(&vals->pci, val);
            } else if (!strcmp(name, "cell_id")) {
                set_str(&vals->cell_id, val);
            } else if (!strcmp(name, "tac")) {
                set_str(&vals->tac, val);
            } else if (!strcmp(name, "ho_active")) {
                set_str(&vals->ho_active, val);
            } else if (!strcmp(name, "a3_offset")) {
                set_str(&vals->a3_offset, val);
            } else if (!strcmp(name, "time_to_trigger")) {
                set_str(&vals->time_to_trigger, val);
            } else if (!strcmp(name, "hysteresis")) {
                set_str(&vals->hysteresis, val);

            } else if (!strcmp(name, "sched_policy")) {
                set_str(&vals->sched_policy, val);
            } else if (!strcmp(name, "pdsch_max_mcs")) {
                set_str(&vals->pdsch_max_mcs, val);
            } else if (!strcmp(name, "pusch_max_mcs")) {
                set_str(&vals->pusch_max_mcs, val);
            } else if (!strcmp(name, "target_bler")) {
                set_str(&vals->target_bler, val);
            } else if (!strcmp(name, "min_nof_ctrl_symbols")) {
                set_str(&vals->min_nof_ctrl_symbols, val);
            } else if (!strcmp(name, "max_nof_ctrl_symbols")) {
                set_str(&vals->max_nof_ctrl_symbols, val);

            } else if (!strcmp(name, "q_rx_lev_min")) {
                set_str(&vals->q_rx_lev_min, val);
            } else if (!strcmp(name, "cell_barred")) {
                set_str(&vals->cell_barred, val);
            } else if (!strcmp(name, "num_ra_preambles")) {
                set_str(&vals->num_ra_preambles, val);
            } else if (!strcmp(name, "preamble_init_rx_target_pwr")) {
                set_str(&vals->preamble_init_rx_target_pwr, val);
            } else if (!strcmp(name, "pwr_ramping_step")) {
                set_str(&vals->pwr_ramping_step, val);
            } else if (!strcmp(name, "reference_signal_power")) {
                set_str(&vals->reference_signal_power, val);
            } else if (!strcmp(name, "p0_nominal_pusch")) {
                set_str(&vals->p0_nominal_pusch, val);
            } else if (!strcmp(name, "p0_nominal_pucch")) {
                set_str(&vals->p0_nominal_pucch, val);
            } else if (!strcmp(name, "alpha")) {
                set_str(&vals->alpha, val);
            } else if (!strcmp(name, "default_paging_cycle")) {
                set_str(&vals->default_paging_cycle, val);
            } else if (!strcmp(name, "t300")) {
                set_str(&vals->t300, val);
            } else if (!strcmp(name, "t301")) {
                set_str(&vals->t301, val);
            } else if (!strcmp(name, "t310")) {
                set_str(&vals->t310, val);
            } else if (!strcmp(name, "n310")) {
                set_str(&vals->n310, val);
            } else if (!strcmp(name, "t311")) {
                set_str(&vals->t311, val);

            } else if (!strcmp(name, "qci")) {
                set_str(&vals->qci, val);
            } else if (!strcmp(name, "discard_timer")) {
                set_str(&vals->discard_timer, val);
            } else if (!strcmp(name, "pdcp_sn_size")) {
                set_str(&vals->pdcp_sn_size, val);
            } else if (!strcmp(name, "t_poll_retx")) {
                set_str(&vals->t_poll_retx, val);
            } else if (!strcmp(name, "max_retx_thresh")) {
                set_str(&vals->max_retx_thresh, val);
            } else if (!strcmp(name, "t_reordering")) {
                set_str(&vals->t_reordering, val);
            } else if (!strcmp(name, "priority")) {
                set_str(&vals->priority, val);

            } else if (!strcmp(name, "pusch_max_its")) {
                set_str(&vals->pusch_max_its, val);
            } else if (!strcmp(name, "nr_pusch_max_its")) {
                set_str(&vals->nr_pusch_max_its, val);
            } else if (!strcmp(name, "pusch_8bit_decoder")) {
                set_str(&vals->pusch_8bit_decoder, val);
            } else if (!strcmp(name, "nof_phy_threads")) {
                set_str(&vals->nof_phy_threads, val);
            } else if (!strcmp(name, "metrics_period_secs")) {
                set_str(&vals->metrics_period_secs, val);
            } else if (!strcmp(name, "tx_amplitude")) {
                set_str(&vals->tx_amplitude, val);
            } else if (!strcmp(name, "rrc_inactivity_timer")) {
                set_str(&vals->rrc_inactivity_timer, val);
            } else if (!strcmp(name, "rlf_release_timer_ms")) {
                set_str(&vals->rlf_release_timer_ms, val);
            } else if (!strcmp(name, "eea_pref_list")) {
                set_str(&vals->eea_pref_list, val);
            } else if (!strcmp(name, "eia_pref_list")) {
                set_str(&vals->eia_pref_list, val);
            } else if (!strcmp(name, "gtpu_tunnel_timeout")) {
                set_str(&vals->gtpu_tunnel_timeout, val);
            } else if (!strcmp(name, "s1_setup_max_retries")) {
                set_str(&vals->s1_setup_max_retries, val);
            } else if (!strcmp(name, "s1_connect_timer")) {
                set_str(&vals->s1_connect_timer, val);
            } else if (!strcmp(name, "rx_gain_offset")) {
                set_str(&vals->rx_gain_offset, val);
            } else if (!strcmp(name, "use_cedron_f_est_alg")) {
                set_str(&vals->use_cedron_f_est_alg, val);
            } else if (!strcmp(name, "rlf_min_ul_snr_estim")) {
                set_str(&vals->rlf_min_ul_snr_estim, val);
            } else if (!strcmp(name, "max_mac_dl_kos")) {
                set_str(&vals->max_mac_dl_kos, val);
            } else if (!strcmp(name, "max_mac_ul_kos")) {
                set_str(&vals->max_mac_ul_kos, val);
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

static int append_change_bool(char *buf, size_t cap, size_t *off, const char *key, const char *val, int *first) {
    if (!val || !val[0]) {
        return 0;
    }
    if (strcmp(val, "true") && strcmp(val, "false")) {
        return -1;
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
    n = snprintf(buf + *off, cap - *off, "\"%s\":%s", key, val);
    if (n < 0 || *off + (size_t)n >= cap) {
        return -1;
    }
    *off += (size_t)n;
    return 0;
}

static int append_qci_profile_changes(const struct lyd_node *root, char *buf, size_t cap, size_t *off, int *first) {
    if (!root) {
        return 0;
    }
    const struct lyd_node *it;
    LY_LIST_FOR(root, it) {
        if (!strcmp(LYD_NAME(it), "qci_profiles")) {
            const char *qci = child_value(it, "qci");
            if (qci && qci[0]) {
                const struct lyd_node *leaf;
                LY_LIST_FOR(lyd_child(it), leaf) {
                    const char *lname = LYD_NAME(leaf);
                    if (!lname || !strcmp(lname, "qci")) {
                        continue;
                    }
                    const char *lval = lyd_get_value(leaf);
                    if (!lval || !lval[0]) {
                        continue;
                    }
                    char key[128];
                    snprintf(key, sizeof(key), "qci_profiles[%s].%s", qci, lname);
                    if (is_boolean_leaf(lname)) {
                        if (append_change_bool(buf, cap, off, key, lval, first)) {
                            return -1;
                        }
                    } else if (is_numeric_leaf(lname)) {
                        if (append_change(buf, cap, off, key, lval, 1, first)) {
                            return -1;
                        }
                    } else {
                        if (append_change(buf, cap, off, key, lval, 0, first)) {
                            return -1;
                        }
                    }
                }
            }
        }
        if (append_qci_profile_changes(lyd_child(it), buf, cap, off, first)) {
            return -1;
        }
    }
    return 0;
}

static char *build_edit_payload(struct edit_values *vals, const struct lyd_node *edit_root) {
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
        append_change(buf, 8192, &off, "enb_id", vals->enb_id, 0, &first) ||
        append_change(buf, 8192, &off, "mcc", vals->mcc, 0, &first) ||
        append_change(buf, 8192, &off, "mnc", vals->mnc, 0, &first) ||
        append_change(buf, 8192, &off, "mme_addr", vals->mme_addr, 0, &first) ||
        append_change(buf, 8192, &off, "gtp_bind_addr", vals->gtp_bind_addr, 0, &first) ||
        append_change(buf, 8192, &off, "s1c_bind_addr", vals->s1c_bind_addr, 0, &first) ||
        append_change(buf, 8192, &off, "s1c_bind_port", vals->s1c_bind_port, 1, &first) ||
        append_change(buf, 8192, &off, "n_prb", vals->n_prb, 1, &first) ||
        append_change(buf, 8192, &off, "tm", vals->tm, 1, &first) ||
        append_change(buf, 8192, &off, "tx_gain", vals->tx_gain, 1, &first) ||
        append_change(buf, 8192, &off, "rx_gain", vals->rx_gain, 1, &first) ||
        append_change(buf, 8192, &off, "time_adv_nsamples", vals->time_adv_nsamples, 0, &first) ||
        append_change(buf, 8192, &off, "device_name", vals->device_name, 0, &first) ||
        append_change(buf, 8192, &off, "device_args", vals->device_args, 0, &first) ||
        append_change(buf, 8192, &off, "cell_id", vals->cell_id, 0, &first) ||
        append_change(buf, 8192, &off, "tac", vals->tac, 0, &first) ||
        append_change(buf, 8192, &off, "dl_earfcn", vals->dl_earfcn, 1, &first) ||
        append_change(buf, 8192, &off, "pci", vals->pci, 1, &first) ||
        append_change_bool(buf, 8192, &off, "ho_active", vals->ho_active, &first) ||
        append_change(buf, 8192, &off, "a3_offset", vals->a3_offset, 1, &first) ||
        append_change(buf, 8192, &off, "time_to_trigger", vals->time_to_trigger, 1, &first) ||
        append_change(buf, 8192, &off, "hysteresis", vals->hysteresis, 1, &first) ||
        append_change(buf, 8192, &off, "sched_policy", vals->sched_policy, 0, &first) ||
        append_change(buf, 8192, &off, "pdsch_max_mcs", vals->pdsch_max_mcs, 1, &first) ||
        append_change(buf, 8192, &off, "pusch_max_mcs", vals->pusch_max_mcs, 1, &first) ||
        append_change(buf, 8192, &off, "target_bler", vals->target_bler, 1, &first) ||
        append_change(buf, 8192, &off, "min_nof_ctrl_symbols", vals->min_nof_ctrl_symbols, 1, &first) ||
        append_change(buf, 8192, &off, "max_nof_ctrl_symbols", vals->max_nof_ctrl_symbols, 1, &first) ||
        append_change(buf, 8192, &off, "q_rx_lev_min", vals->q_rx_lev_min, 1, &first) ||
        append_change(buf, 8192, &off, "cell_barred", vals->cell_barred, 0, &first) ||
        append_change(buf, 8192, &off, "num_ra_preambles", vals->num_ra_preambles, 1, &first) ||
        append_change(buf, 8192, &off, "preamble_init_rx_target_pwr", vals->preamble_init_rx_target_pwr, 1, &first) ||
        append_change(buf, 8192, &off, "pwr_ramping_step", vals->pwr_ramping_step, 1, &first) ||
        append_change(buf, 8192, &off, "reference_signal_power", vals->reference_signal_power, 1, &first) ||
        append_change(buf, 8192, &off, "p0_nominal_pusch", vals->p0_nominal_pusch, 1, &first) ||
        append_change(buf, 8192, &off, "p0_nominal_pucch", vals->p0_nominal_pucch, 1, &first) ||
        append_change(buf, 8192, &off, "alpha", vals->alpha, 1, &first) ||
        append_change(buf, 8192, &off, "default_paging_cycle", vals->default_paging_cycle, 1, &first) ||
        append_change(buf, 8192, &off, "t300", vals->t300, 1, &first) ||
        append_change(buf, 8192, &off, "t301", vals->t301, 1, &first) ||
        append_change(buf, 8192, &off, "t310", vals->t310, 1, &first) ||
        append_change(buf, 8192, &off, "n310", vals->n310, 1, &first) ||
        append_change(buf, 8192, &off, "t311", vals->t311, 1, &first) ||
        append_change(buf, 8192, &off, "qci", vals->qci, 1, &first) ||
        append_change(buf, 8192, &off, "discard_timer", vals->discard_timer, 1, &first) ||
        append_change(buf, 8192, &off, "pdcp_sn_size", vals->pdcp_sn_size, 1, &first) ||
        append_change(buf, 8192, &off, "t_poll_retx", vals->t_poll_retx, 1, &first) ||
        append_change(buf, 8192, &off, "max_retx_thresh", vals->max_retx_thresh, 1, &first) ||
        append_change(buf, 8192, &off, "t_reordering", vals->t_reordering, 1, &first) ||
        append_change(buf, 8192, &off, "priority", vals->priority, 1, &first) ||
        append_change(buf, 8192, &off, "pusch_max_its", vals->pusch_max_its, 1, &first) ||
        append_change(buf, 8192, &off, "nr_pusch_max_its", vals->nr_pusch_max_its, 1, &first) ||
        append_change_bool(buf, 8192, &off, "pusch_8bit_decoder", vals->pusch_8bit_decoder, &first) ||
        append_change(buf, 8192, &off, "nof_phy_threads", vals->nof_phy_threads, 1, &first) ||
        append_change(buf, 8192, &off, "metrics_period_secs", vals->metrics_period_secs, 1, &first) ||
        append_change(buf, 8192, &off, "tx_amplitude", vals->tx_amplitude, 1, &first) ||
        append_change(buf, 8192, &off, "rrc_inactivity_timer", vals->rrc_inactivity_timer, 1, &first) ||
        append_change(buf, 8192, &off, "rlf_release_timer_ms", vals->rlf_release_timer_ms, 1, &first) ||
        append_change(buf, 8192, &off, "eea_pref_list", vals->eea_pref_list, 0, &first) ||
        append_change(buf, 8192, &off, "eia_pref_list", vals->eia_pref_list, 0, &first) ||
        append_change(buf, 8192, &off, "gtpu_tunnel_timeout", vals->gtpu_tunnel_timeout, 1, &first) ||
        append_change(buf, 8192, &off, "s1_setup_max_retries", vals->s1_setup_max_retries, 1, &first) ||
        append_change(buf, 8192, &off, "s1_connect_timer", vals->s1_connect_timer, 1, &first) ||
        append_change(buf, 8192, &off, "rx_gain_offset", vals->rx_gain_offset, 1, &first) ||
        append_change_bool(buf, 8192, &off, "use_cedron_f_est_alg", vals->use_cedron_f_est_alg, &first) ||
        append_change(buf, 8192, &off, "rlf_min_ul_snr_estim", vals->rlf_min_ul_snr_estim, 1, &first) ||
        append_change(buf, 8192, &off, "max_mac_dl_kos", vals->max_mac_dl_kos, 1, &first) ||
        append_change(buf, 8192, &off, "max_mac_ul_kos", vals->max_mac_ul_kos, 1, &first)) {
        free(buf);
        return NULL;
    }

    if (append_qci_profile_changes(edit_root, buf, 8192, &off, &first)) {
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

    /* boolean */
    if (!strncmp(p, "true", 4)) {
        return strdup("true");
    }
    if (!strncmp(p, "false", 5)) {
        return strdup("false");
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

static char *extract_array_section(const char *json, const char *key) {
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
    if (*p != '[') {
        return NULL;
    }
    const char *start = p;
    int depth = 0;
    for (const char *q = p; *q; q++) {
        if (*q == '[') depth++;
        else if (*q == ']') {
            depth--;
            if (depth == 0) {
                size_t len = (size_t)(q - start + 1);
                char *out = malloc(len + 1);
                if (!out) return NULL;
                memcpy(out, start, len);
                out[len] = '\0';
                return out;
            }
        }
    }
    return NULL;
}

static char **extract_object_list_from_array(const char *array_json, size_t *count) {
    if (count) *count = 0;
    if (!array_json || array_json[0] != '[') {
        return NULL;
    }
    size_t cap = 8;
    size_t n = 0;
    char **out = calloc(cap, sizeof(char *));
    if (!out) return NULL;

    int depth = 0;
    const char *obj_start = NULL;
    for (const char *p = array_json; *p; p++) {
        if (*p == '{') {
            depth++;
            if (depth == 1) {
                obj_start = p;
            }
        } else if (*p == '}') {
            if (depth == 1 && obj_start) {
                size_t len = (size_t)(p - obj_start + 1);
                char *obj = malloc(len + 1);
                if (!obj) break;
                memcpy(obj, obj_start, len);
                obj[len] = '\0';
                if (n == cap) {
                    cap *= 2;
                    char **tmp = realloc(out, cap * sizeof(char *));
                    if (!tmp) {
                        free(obj);
                        break;
                    }
                    out = tmp;
                }
                out[n++] = obj;
                obj_start = NULL;
            }
            if (depth > 0) depth--;
        }
    }
    if (count) *count = n;
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
    char *payload = build_edit_payload(&vals, edit_tree);
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

    char *enb_id = extract_kv_value(json, "enb_id");
    char *mcc = extract_kv_value(json, "mcc");
    char *mnc = extract_kv_value(json, "mnc");
    char *mme_addr = extract_kv_value(json, "mme_addr");
    char *gtp_bind_addr = extract_kv_value(json, "gtp_bind_addr");
    char *s1c_bind_addr = extract_kv_value(json, "s1c_bind_addr");
    char *s1c_bind_port = extract_kv_value(json, "s1c_bind_port");
    char *n_prb = extract_kv_value(json, "n_prb");
    char *tm = extract_kv_value(json, "tm");

    char *cell_id_json = extract_kv_value(json, "cell_id");
    char *tac = extract_kv_value(json, "tac");
    char *dl_earfcn = extract_kv_value(json, "dl_earfcn");
    char *pci = extract_kv_value(json, "pci");
    char *ho_active = extract_kv_value(json, "ho_active");
    char *a3_offset = extract_kv_value(json, "a3_offset");
    char *time_to_trigger = extract_kv_value(json, "time_to_trigger");
    char *hysteresis = extract_kv_value(json, "hysteresis");

    char *enb_serial = extract_kv_value(json, "enb_serial");
    char *tx_gain = extract_kv_value(json, "tx_gain");
    char *rx_gain = extract_kv_value(json, "rx_gain");
    char *time_adv_nsamples = extract_kv_value(json, "time_adv_nsamples");
    char *device_name = extract_kv_value(json, "device_name");
    char *device_args = extract_kv_value(json, "device_args");

    char *sched_policy = extract_kv_value(json, "sched_policy");
    char *pdsch_max_mcs = extract_kv_value(json, "pdsch_max_mcs");
    char *pusch_max_mcs = extract_kv_value(json, "pusch_max_mcs");
    char *target_bler = extract_kv_value(json, "target_bler");
    char *min_nof_ctrl_symbols = extract_kv_value(json, "min_nof_ctrl_symbols");
    char *max_nof_ctrl_symbols = extract_kv_value(json, "max_nof_ctrl_symbols");

    char *q_rx_lev_min = extract_kv_value(json, "q_rx_lev_min");
    char *cell_barred = extract_kv_value(json, "cell_barred");
    char *num_ra_preambles = extract_kv_value(json, "num_ra_preambles");
    char *preamble_init_rx_target_pwr = extract_kv_value(json, "preamble_init_rx_target_pwr");
    char *pwr_ramping_step = extract_kv_value(json, "pwr_ramping_step");
    char *reference_signal_power = extract_kv_value(json, "reference_signal_power");
    char *p0_nominal_pusch = extract_kv_value(json, "p0_nominal_pusch");
    char *p0_nominal_pucch = extract_kv_value(json, "p0_nominal_pucch");
    char *alpha = extract_kv_value(json, "alpha");
    char *default_paging_cycle = extract_kv_value(json, "default_paging_cycle");
    char *t300 = extract_kv_value(json, "t300");
    char *t301 = extract_kv_value(json, "t301");
    char *t310 = extract_kv_value(json, "t310");
    char *n310 = extract_kv_value(json, "n310");
    char *t311 = extract_kv_value(json, "t311");

    char *qci = extract_kv_value(json, "qci");
    char *discard_timer = extract_kv_value(json, "discard_timer");
    char *pdcp_sn_size = extract_kv_value(json, "pdcp_sn_size");
    char *t_poll_retx = extract_kv_value(json, "t_poll_retx");
    char *max_retx_thresh = extract_kv_value(json, "max_retx_thresh");
    char *t_reordering = extract_kv_value(json, "t_reordering");
    char *priority = extract_kv_value(json, "priority");

    char *pusch_max_its = extract_kv_value(json, "pusch_max_its");
    char *nr_pusch_max_its = extract_kv_value(json, "nr_pusch_max_its");
    char *pusch_8bit_decoder = extract_kv_value(json, "pusch_8bit_decoder");
    char *nof_phy_threads = extract_kv_value(json, "nof_phy_threads");
    char *metrics_period_secs = extract_kv_value(json, "metrics_period_secs");
    char *tx_amplitude = extract_kv_value(json, "tx_amplitude");
    char *rrc_inactivity_timer = extract_kv_value(json, "rrc_inactivity_timer");
    char *rlf_release_timer_ms = extract_kv_value(json, "rlf_release_timer_ms");
    char *eea_pref_list = extract_kv_value(json, "eea_pref_list");
    char *eia_pref_list = extract_kv_value(json, "eia_pref_list");
    char *gtpu_tunnel_timeout = extract_kv_value(json, "gtpu_tunnel_timeout");
    char *s1_setup_max_retries = extract_kv_value(json, "s1_setup_max_retries");
    char *s1_connect_timer = extract_kv_value(json, "s1_connect_timer");
    char *rx_gain_offset = extract_kv_value(json, "rx_gain_offset");
    char *use_cedron_f_est_alg = extract_kv_value(json, "use_cedron_f_est_alg");
    char *rlf_min_ul_snr_estim = extract_kv_value(json, "rlf_min_ul_snr_estim");
    char *max_mac_dl_kos = extract_kv_value(json, "max_mac_dl_kos");
    char *max_mac_ul_kos = extract_kv_value(json, "max_mac_ul_kos");

    char *qci_profiles_arr = extract_array_section(json, "qci_profiles");
    size_t qci_profiles_n = 0;
    char **qci_profiles = extract_object_list_from_array(qci_profiles_arr, &qci_profiles_n);

    if (cell_id_json && cell_id_json[0]) {
        free(cell_id);
        cell_id = strdup(cell_id_json);
    }

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

    if (enb_id && enb_id[0]) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:enb_id",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, enb_id, 0, NULL);
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
    if (mme_addr && mme_addr[0]) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:mme_addr",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, mme_addr, 0, NULL);
    }
    if (gtp_bind_addr && gtp_bind_addr[0]) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:gtp_bind_addr",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, gtp_bind_addr, 0, NULL);
    }
    if (s1c_bind_addr && s1c_bind_addr[0]) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:s1c_bind_addr",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, s1c_bind_addr, 0, NULL);
    }
    if (s1c_bind_port && s1c_bind_port[0] && strcmp(s1c_bind_port, "0")) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:s1c_bind_port",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, s1c_bind_port, 0, NULL);
    }
    if (tm && tm[0] && strcmp(tm, "0")) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:tm",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, tm, 0, NULL);
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
    if (rx_gain && rx_gain[0] && strcmp(rx_gain, "0")) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:rx_gain",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, rx_gain, 0, NULL);
    }
    if (time_adv_nsamples && time_adv_nsamples[0]) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:time_adv_nsamples",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, time_adv_nsamples, 0, NULL);
    }
    if (device_name && device_name[0]) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:device_name",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, device_name, 0, NULL);
    }
    if (device_args && device_args[0]) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:device_args",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, device_args, 0, NULL);
    }

    // scheduler container.
    if ((sched_policy && sched_policy[0]) || (pdsch_max_mcs && pdsch_max_mcs[0]) || (pusch_max_mcs && pusch_max_mcs[0]) ||
        (target_bler && target_bler[0] && strcmp(target_bler, "0")) ||
        (min_nof_ctrl_symbols && min_nof_ctrl_symbols[0] && strcmp(min_nof_ctrl_symbols, "0")) ||
        (max_nof_ctrl_symbols && max_nof_ctrl_symbols[0] && strcmp(max_nof_ctrl_symbols, "0"))) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:scheduler",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, NULL, 0, NULL);
        if (sched_policy && sched_policy[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:scheduler/srsran-vendor-ext:sched_policy",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, sched_policy, 0, NULL);
        }
        if (pdsch_max_mcs && pdsch_max_mcs[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:scheduler/srsran-vendor-ext:pdsch_max_mcs",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, pdsch_max_mcs, 0, NULL);
        }
        if (pusch_max_mcs && pusch_max_mcs[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:scheduler/srsran-vendor-ext:pusch_max_mcs",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, pusch_max_mcs, 0, NULL);
        }
        if (target_bler && target_bler[0] && strcmp(target_bler, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:scheduler/srsran-vendor-ext:target_bler",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, target_bler, 0, NULL);
        }
        if (min_nof_ctrl_symbols && min_nof_ctrl_symbols[0] && strcmp(min_nof_ctrl_symbols, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:scheduler/srsran-vendor-ext:min_nof_ctrl_symbols",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, min_nof_ctrl_symbols, 0, NULL);
        }
        if (max_nof_ctrl_symbols && max_nof_ctrl_symbols[0] && strcmp(max_nof_ctrl_symbols, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:scheduler/srsran-vendor-ext:max_nof_ctrl_symbols",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, max_nof_ctrl_symbols, 0, NULL);
        }
    }

    // sib container.
    if ((q_rx_lev_min && q_rx_lev_min[0]) || (cell_barred && cell_barred[0]) || (num_ra_preambles && num_ra_preambles[0]) ||
        (preamble_init_rx_target_pwr && preamble_init_rx_target_pwr[0]) || (pwr_ramping_step && pwr_ramping_step[0]) ||
        (reference_signal_power && reference_signal_power[0] && strcmp(reference_signal_power, "0")) ||
        (p0_nominal_pusch && p0_nominal_pusch[0]) || (p0_nominal_pucch && p0_nominal_pucch[0]) ||
        (alpha && alpha[0] && strcmp(alpha, "0")) || (default_paging_cycle && default_paging_cycle[0]) ||
        (t300 && t300[0] && strcmp(t300, "0")) || (t301 && t301[0] && strcmp(t301, "0")) || (t310 && t310[0] && strcmp(t310, "0")) ||
        (n310 && n310[0] && strcmp(n310, "0")) || (t311 && t311[0] && strcmp(t311, "0"))) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, NULL, 0, NULL);
        if (q_rx_lev_min && q_rx_lev_min[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:q_rx_lev_min",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, q_rx_lev_min, 0, NULL);
        }
        if (cell_barred && cell_barred[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:cell_barred",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, cell_barred, 0, NULL);
        }
        if (num_ra_preambles && num_ra_preambles[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:num_ra_preambles",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, num_ra_preambles, 0, NULL);
        }
        if (preamble_init_rx_target_pwr && preamble_init_rx_target_pwr[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:preamble_init_rx_target_pwr",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, preamble_init_rx_target_pwr, 0, NULL);
        }
        if (pwr_ramping_step && pwr_ramping_step[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:pwr_ramping_step",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, pwr_ramping_step, 0, NULL);
        }
        if (reference_signal_power && reference_signal_power[0] && strcmp(reference_signal_power, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:reference_signal_power",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, reference_signal_power, 0, NULL);
        }
        if (p0_nominal_pusch && p0_nominal_pusch[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:p0_nominal_pusch",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, p0_nominal_pusch, 0, NULL);
        }
        if (p0_nominal_pucch && p0_nominal_pucch[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:p0_nominal_pucch",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, p0_nominal_pucch, 0, NULL);
        }
        if (alpha && alpha[0] && strcmp(alpha, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:alpha",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, alpha, 0, NULL);
        }
        if (default_paging_cycle && default_paging_cycle[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:default_paging_cycle",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, default_paging_cycle, 0, NULL);
        }
        if ((t300 && t300[0] && strcmp(t300, "0")) || (t301 && t301[0] && strcmp(t301, "0")) || (t310 && t310[0] && strcmp(t310, "0")) ||
            (n310 && n310[0] && strcmp(n310, "0")) || (t311 && t311[0] && strcmp(t311, "0"))) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:ue_timers_and_constants",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, NULL, 0, NULL);
            if (t300 && t300[0] && strcmp(t300, "0")) {
                snprintf(path, sizeof(path),
                         "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:ue_timers_and_constants/srsran-vendor-ext:t300",
                         sn_id, me_id, fn_id);
                lyd_new_path(tree, ctx, path, t300, 0, NULL);
            }
            if (t301 && t301[0] && strcmp(t301, "0")) {
                snprintf(path, sizeof(path),
                         "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:ue_timers_and_constants/srsran-vendor-ext:t301",
                         sn_id, me_id, fn_id);
                lyd_new_path(tree, ctx, path, t301, 0, NULL);
            }
            if (t310 && t310[0] && strcmp(t310, "0")) {
                snprintf(path, sizeof(path),
                         "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:ue_timers_and_constants/srsran-vendor-ext:t310",
                         sn_id, me_id, fn_id);
                lyd_new_path(tree, ctx, path, t310, 0, NULL);
            }
            if (n310 && n310[0] && strcmp(n310, "0")) {
                snprintf(path, sizeof(path),
                         "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:ue_timers_and_constants/srsran-vendor-ext:n310",
                         sn_id, me_id, fn_id);
                lyd_new_path(tree, ctx, path, n310, 0, NULL);
            }
            if (t311 && t311[0] && strcmp(t311, "0")) {
                snprintf(path, sizeof(path),
                         "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:sib/srsran-vendor-ext:ue_timers_and_constants/srsran-vendor-ext:t311",
                         sn_id, me_id, fn_id);
                lyd_new_path(tree, ctx, path, t311, 0, NULL);
            }
        }
    }

    // rb container
    if (qci || discard_timer || pdcp_sn_size || t_poll_retx || max_retx_thresh || t_reordering || priority) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:rb",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, NULL, 0, NULL);
        if (qci) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:rb/srsran-vendor-ext:qci",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, qci, 0, NULL);
        }
        if (discard_timer) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:rb/srsran-vendor-ext:discard_timer",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, discard_timer, 0, NULL);
        }
        if (pdcp_sn_size) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:rb/srsran-vendor-ext:pdcp_sn_size",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, pdcp_sn_size, 0, NULL);
        }
        if (t_poll_retx) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:rb/srsran-vendor-ext:t_poll_retx",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, t_poll_retx, 0, NULL);
        }
        if (max_retx_thresh) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:rb/srsran-vendor-ext:max_retx_thresh",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, max_retx_thresh, 0, NULL);
        }
        if (t_reordering) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:rb/srsran-vendor-ext:t_reordering",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, t_reordering, 0, NULL);
        }
        if (priority) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:rb/srsran-vendor-ext:priority",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, priority, 0, NULL);
        }
    }

    // expert container
    if ((pusch_max_its && pusch_max_its[0] && strcmp(pusch_max_its, "0")) ||
        (nr_pusch_max_its && nr_pusch_max_its[0] && strcmp(nr_pusch_max_its, "0")) ||
        (pusch_8bit_decoder && pusch_8bit_decoder[0]) ||
        (nof_phy_threads && nof_phy_threads[0] && strcmp(nof_phy_threads, "0")) ||
        (metrics_period_secs && metrics_period_secs[0] && strcmp(metrics_period_secs, "0")) ||
        (tx_amplitude && tx_amplitude[0] && strcmp(tx_amplitude, "0")) ||
        (rrc_inactivity_timer && rrc_inactivity_timer[0] && strcmp(rrc_inactivity_timer, "0")) ||
        (rlf_release_timer_ms && rlf_release_timer_ms[0] && strcmp(rlf_release_timer_ms, "0")) ||
        (eea_pref_list && eea_pref_list[0]) || (eia_pref_list && eia_pref_list[0]) ||
        (gtpu_tunnel_timeout && gtpu_tunnel_timeout[0] && strcmp(gtpu_tunnel_timeout, "0")) ||
        (s1_setup_max_retries && s1_setup_max_retries[0] && strcmp(s1_setup_max_retries, "0")) ||
        (s1_connect_timer && s1_connect_timer[0] && strcmp(s1_connect_timer, "0")) ||
        (rx_gain_offset && rx_gain_offset[0] && strcmp(rx_gain_offset, "0")) ||
        (use_cedron_f_est_alg && use_cedron_f_est_alg[0]) ||
        (rlf_min_ul_snr_estim && rlf_min_ul_snr_estim[0] && strcmp(rlf_min_ul_snr_estim, "0")) ||
        (max_mac_dl_kos && max_mac_dl_kos[0] && strcmp(max_mac_dl_kos, "0")) ||
        (max_mac_ul_kos && max_mac_ul_kos[0] && strcmp(max_mac_ul_kos, "0"))) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert",
                 sn_id, me_id, fn_id);
        lyd_new_path(tree, ctx, path, NULL, 0, NULL);
        if (pusch_max_its && pusch_max_its[0] && strcmp(pusch_max_its, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:pusch_max_its",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, pusch_max_its, 0, NULL);
        }
        if (nr_pusch_max_its && nr_pusch_max_its[0] && strcmp(nr_pusch_max_its, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:nr_pusch_max_its",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, nr_pusch_max_its, 0, NULL);
        }
        if (pusch_8bit_decoder && pusch_8bit_decoder[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:pusch_8bit_decoder",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, pusch_8bit_decoder, 0, NULL);
        }
        if (nof_phy_threads && nof_phy_threads[0] && strcmp(nof_phy_threads, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:nof_phy_threads",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, nof_phy_threads, 0, NULL);
        }
        if (metrics_period_secs && metrics_period_secs[0] && strcmp(metrics_period_secs, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:metrics_period_secs",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, metrics_period_secs, 0, NULL);
        }
        if (tx_amplitude && tx_amplitude[0] && strcmp(tx_amplitude, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:tx_amplitude",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, tx_amplitude, 0, NULL);
        }
        if (rrc_inactivity_timer && rrc_inactivity_timer[0] && strcmp(rrc_inactivity_timer, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:rrc_inactivity_timer",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, rrc_inactivity_timer, 0, NULL);
        }
        if (rlf_release_timer_ms && rlf_release_timer_ms[0] && strcmp(rlf_release_timer_ms, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:rlf_release_timer_ms",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, rlf_release_timer_ms, 0, NULL);
        }
        if (eea_pref_list && eea_pref_list[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:eea_pref_list",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, eea_pref_list, 0, NULL);
        }
        if (eia_pref_list && eia_pref_list[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:eia_pref_list",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, eia_pref_list, 0, NULL);
        }
        if (gtpu_tunnel_timeout && gtpu_tunnel_timeout[0] && strcmp(gtpu_tunnel_timeout, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:gtpu_tunnel_timeout",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, gtpu_tunnel_timeout, 0, NULL);
        }
        if (s1_setup_max_retries && s1_setup_max_retries[0] && strcmp(s1_setup_max_retries, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:s1_setup_max_retries",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, s1_setup_max_retries, 0, NULL);
        }
        if (s1_connect_timer && s1_connect_timer[0] && strcmp(s1_connect_timer, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:s1_connect_timer",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, s1_connect_timer, 0, NULL);
        }
        if (rx_gain_offset && rx_gain_offset[0] && strcmp(rx_gain_offset, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:rx_gain_offset",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, rx_gain_offset, 0, NULL);
        }
        if (use_cedron_f_est_alg && use_cedron_f_est_alg[0]) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:use_cedron_f_est_alg",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, use_cedron_f_est_alg, 0, NULL);
        }
        if (rlf_min_ul_snr_estim && rlf_min_ul_snr_estim[0] && strcmp(rlf_min_ul_snr_estim, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:rlf_min_ul_snr_estim",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, rlf_min_ul_snr_estim, 0, NULL);
        }
        if (max_mac_dl_kos && max_mac_dl_kos[0] && strcmp(max_mac_dl_kos, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:max_mac_dl_kos",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, max_mac_dl_kos, 0, NULL);
        }
        if (max_mac_ul_kos && max_mac_ul_kos[0] && strcmp(max_mac_ul_kos, "0")) {
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:expert/srsran-vendor-ext:max_mac_ul_kos",
                     sn_id, me_id, fn_id);
            lyd_new_path(tree, ctx, path, max_mac_ul_kos, 0, NULL);
        }
    }

    // qci_profiles list
    for (size_t i = 0; i < qci_profiles_n; i++) {
        char *qci_v = extract_kv_value(qci_profiles[i], "qci");
        if (!qci_v || !qci_v[0]) {
            free(qci_v);
            continue;
        }
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:qci_profiles[qci='%s']",
                 sn_id, me_id, fn_id, qci_v);
        lyd_new_path(tree, ctx, path, NULL, 0, NULL);

        const char *fields[] = {"discard_timer", "pdcp_sn_size", "t_poll_retx", "max_retx_thresh", "t_reordering", "priority"};
        for (size_t j = 0; j < sizeof(fields) / sizeof(fields[0]); j++) {
            char *vv = extract_kv_value(qci_profiles[i], fields[j]);
            if (!vv) continue;
            snprintf(path, sizeof(path),
                     "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/srsran-vendor-ext:qci_profiles[qci='%s']/srsran-vendor-ext:%s",
                     sn_id, me_id, fn_id, qci_v, fields[j]);
            lyd_new_path(tree, ctx, path, vv, 0, NULL);
            free(vv);
        }
        free(qci_v);
    }

    snprintf(path, sizeof(path),
             "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:EUtranCell[id='%s']",
             sn_id, me_id, fn_id, cell_id);
    lyd_new_path(tree, ctx, path, NULL, 0, NULL);

    if (tac) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:EUtranCell[id='%s']/_3gpp-common-managed-element:tac",
                 sn_id, me_id, fn_id, cell_id);
        lyd_new_path(tree, ctx, path, tac, 0, NULL);
    }

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
    if (ho_active) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:EUtranCell[id='%s']/_3gpp-common-managed-element:ho_active",
                 sn_id, me_id, fn_id, cell_id);
        lyd_new_path(tree, ctx, path, ho_active, 0, NULL);
    }
    if (a3_offset) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:EUtranCell[id='%s']/_3gpp-common-managed-element:a3_offset",
                 sn_id, me_id, fn_id, cell_id);
        lyd_new_path(tree, ctx, path, a3_offset, 0, NULL);
    }
    if (time_to_trigger) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:EUtranCell[id='%s']/_3gpp-common-managed-element:time_to_trigger",
                 sn_id, me_id, fn_id, cell_id);
        lyd_new_path(tree, ctx, path, time_to_trigger, 0, NULL);
    }
    if (hysteresis) {
        snprintf(path, sizeof(path),
                 "/_3gpp-common-managed-element:SubNetwork[id='%s']/_3gpp-common-managed-element:ManagedElement[id='%s']/_3gpp-common-managed-element:ENBFunction[id='%s']/_3gpp-common-managed-element:EUtranCell[id='%s']/_3gpp-common-managed-element:hysteresis",
                 sn_id, me_id, fn_id, cell_id);
        lyd_new_path(tree, ctx, path, hysteresis, 0, NULL);
    }

cleanup:
    free(sn_id);
    free(me_id);
    free(fn_id);
    free(cell_id);
    free(enb_id);
    free(mcc);
    free(mnc);
    free(mme_addr);
    free(gtp_bind_addr);
    free(s1c_bind_addr);
    free(s1c_bind_port);
    free(n_prb);
    free(tm);
    free(cell_id_json);
    free(tac);
    free(dl_earfcn);
    free(pci);
    free(ho_active);
    free(a3_offset);
    free(time_to_trigger);
    free(hysteresis);
    free(enb_serial);
    free(tx_gain);
    free(rx_gain);
    free(time_adv_nsamples);
    free(device_name);
    free(device_args);

    free(sched_policy);
    free(pdsch_max_mcs);
    free(pusch_max_mcs);
    free(target_bler);
    free(min_nof_ctrl_symbols);
    free(max_nof_ctrl_symbols);

    free(q_rx_lev_min);
    free(cell_barred);
    free(num_ra_preambles);
    free(preamble_init_rx_target_pwr);
    free(pwr_ramping_step);
    free(reference_signal_power);
    free(p0_nominal_pusch);
    free(p0_nominal_pucch);
    free(alpha);
    free(default_paging_cycle);
    free(t300);
    free(t301);
    free(t310);
    free(n310);
    free(t311);

    free(qci);
    free(discard_timer);
    free(pdcp_sn_size);
    free(t_poll_retx);
    free(max_retx_thresh);
    free(t_reordering);
    free(priority);

    free(pusch_max_its);
    free(nr_pusch_max_its);
    free(pusch_8bit_decoder);
    free(nof_phy_threads);
    free(metrics_period_secs);
    free(tx_amplitude);
    free(rrc_inactivity_timer);
    free(rlf_release_timer_ms);
    free(eea_pref_list);
    free(eia_pref_list);
    free(gtpu_tunnel_timeout);
    free(s1_setup_max_retries);
    free(s1_connect_timer);
    free(rx_gain_offset);
    free(use_cedron_f_est_alg);
    free(rlf_min_ul_snr_estim);
    free(max_mac_dl_kos);
    free(max_mac_ul_kos);

    if (qci_profiles) {
        for (size_t i = 0; i < qci_profiles_n; i++) {
            free(qci_profiles[i]);
        }
        free(qci_profiles);
    }
    free(qci_profiles_arr);

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
