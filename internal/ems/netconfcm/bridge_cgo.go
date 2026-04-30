//go:build netconf && cgo

package netconfcm

/*
#cgo pkg-config: libyang
#include <dirent.h>
#include <errno.h>
#include <limits.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <libyang/libyang.h>

typedef struct {
    char *path;
    char *value;
    int is_key;
} cm_leaf;

typedef struct {
    cm_leaf *items;
    size_t count;
    size_t cap;
    char *errmsg;
} cm_result;

static void cm_set_error(cm_result *res, const char *msg) {
    if (!res || res->errmsg) {
        return;
    }
    res->errmsg = strdup(msg ? msg : "unknown error");
}

static int cm_add_searchdirs(struct ly_ctx *ctx, const char *pathlist) {
    if (!ctx || !pathlist || !pathlist[0]) {
        return 0;
    }
    char *copy = strdup(pathlist);
    if (!copy) {
        return -1;
    }
    for (char *tok = strtok(copy, ":"); tok; tok = strtok(NULL, ":")) {
        if (!tok[0]) {
            continue;
        }
        if (ly_ctx_set_searchdir(ctx, tok)) {
            free(copy);
            return -1;
        }
    }
    free(copy);
    return 0;
}

static int cm_parse_all_yang(struct ly_ctx *ctx, const char *pathlist) {
    if (!ctx || !pathlist || !pathlist[0]) {
        return -1;
    }
    char *copy = strdup(pathlist);
    if (!copy) {
        return -1;
    }
    for (char *dir = strtok(copy, ":"); dir; dir = strtok(NULL, ":")) {
        DIR *dh = opendir(dir);
        if (!dh) {
            continue;
        }
        struct dirent *de = NULL;
        while ((de = readdir(dh)) != NULL) {
            const char *name = de->d_name;
            size_t len = strlen(name);
            if (len < 5 || strcmp(name + len - 5, ".yang")) {
                continue;
            }
            char path[PATH_MAX];
            if (snprintf(path, sizeof(path), "%s/%s", dir, name) >= (int)sizeof(path)) {
                closedir(dh);
                free(copy);
                return -1;
            }
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

static int cm_append_leaf(cm_result *res, const char *path, const char *value, int is_key) {
    if (!res || !path || !value) {
        return -1;
    }
    if (res->count == res->cap) {
        size_t next_cap = res->cap ? res->cap * 2 : 16;
        cm_leaf *next = (cm_leaf *)realloc(res->items, next_cap * sizeof(*next));
        if (!next) {
            return -1;
        }
        res->items = next;
        res->cap = next_cap;
    }
    cm_leaf *item = &res->items[res->count++];
    item->path = strdup(path);
    item->value = strdup(value);
    item->is_key = is_key;
    if (!item->path || !item->value) {
        return -1;
    }
    return 0;
}

static int cm_walk_tree(const struct lyd_node *node, cm_result *res) {
    const struct lyd_node *it = NULL;
    LY_LIST_FOR(node, it) {
        if (!it->schema) {
            cm_set_error(res, "encountered node without schema");
            return -1;
        }
        const struct lysc_node *schema = it->schema;
        if ((schema->nodetype == LYS_LEAF) || (schema->nodetype == LYS_LEAFLIST)) {
            if (!(schema->flags & LYS_CONFIG_W)) {
                char *path = lyd_path(it, LYD_PATH_STD, NULL, 0);
                if (path) {
                    char msg[1024];
                    snprintf(msg, sizeof(msg), "path %s is not config true", path);
                    cm_set_error(res, msg);
                    free(path);
                } else {
                    cm_set_error(res, "encountered non-config leaf in edit payload");
                }
                return -1;
            }
            char *path = lyd_path(it, LYD_PATH_STD, NULL, 0);
            const char *value = lyd_get_value(it);
            if (!path || !value || cm_append_leaf(res, path, value, (schema->flags & LYS_KEY) ? 1 : 0)) {
                free(path);
                cm_set_error(res, "failed to collect parsed leaf");
                return -1;
            }
            free(path);
        }
        const struct lyd_node *child = lyd_child(it);
        if (child && cm_walk_tree(child, res)) {
            return -1;
        }
    }
    return 0;
}

static int cm_prepare_ctx(const char *yang_dir, struct ly_ctx **ctx, char **errmsg) {
    if (ly_ctx_new(NULL, LY_CTX_NO_YANGLIBRARY, ctx)) {
        if (errmsg) {
            *errmsg = strdup("failed to create libyang context");
        }
        return -1;
    }
    if (cm_add_searchdirs(*ctx, yang_dir) || cm_parse_all_yang(*ctx, yang_dir)) {
        if (errmsg) {
            const char *msg = ly_errmsg(*ctx);
            *errmsg = strdup(msg ? msg : "failed to load YANG modules");
        }
        ly_ctx_destroy(*ctx);
        *ctx = NULL;
        return -1;
    }
    return 0;
}

static int cm_parse_tree(const char *yang_dir, const char *json, struct ly_ctx **ctx, struct lyd_node **tree, char **errmsg) {
    if (cm_prepare_ctx(yang_dir, ctx, errmsg)) {
        return -1;
    }
    if (lyd_parse_data_mem(*ctx, json, LYD_JSON, LYD_PARSE_STRICT, 0, tree)) {
        if (errmsg) {
            const char *msg = ly_errmsg(*ctx);
            *errmsg = strdup(msg ? msg : "failed to parse YANG-JSON");
        }
        ly_ctx_destroy(*ctx);
        *ctx = NULL;
        return -1;
    }
    if (lyd_validate_all(tree, *ctx, LYD_VALIDATE_PRESENT, NULL)) {
        if (errmsg) {
            const char *msg = ly_errmsg(*ctx);
            *errmsg = strdup(msg ? msg : "YANG validation failed");
        }
        lyd_free_all(*tree);
        *tree = NULL;
        ly_ctx_destroy(*ctx);
        *ctx = NULL;
        return -1;
    }
    return 0;
}

int cm_extract_leafs(const char *yang_dir, const char *json, cm_result *res) {
    struct ly_ctx *ctx = NULL;
    struct lyd_node *tree = NULL;
    if (!res) {
        return -1;
    }
    memset(res, 0, sizeof(*res));
    if (cm_parse_tree(yang_dir, json, &ctx, &tree, &res->errmsg)) {
        return -1;
    }
    if (cm_walk_tree(tree, res)) {
        lyd_free_all(tree);
        ly_ctx_destroy(ctx);
        if (!res->errmsg) {
            res->errmsg = strdup("failed to walk parsed tree");
        }
        return -1;
    }
    lyd_free_all(tree);
    ly_ctx_destroy(ctx);
    return 0;
}

int cm_validate_json(const char *yang_dir, const char *json, char **errmsg) {
    struct ly_ctx *ctx = NULL;
    struct lyd_node *tree = NULL;
    if (errmsg) {
        *errmsg = NULL;
    }
    if (cm_parse_tree(yang_dir, json, &ctx, &tree, errmsg)) {
        return -1;
    }
    lyd_free_all(tree);
    ly_ctx_destroy(ctx);
    return 0;
}

void cm_result_free(cm_result *res) {
    size_t i;
    if (!res) {
        return;
    }
    for (i = 0; i < res->count; ++i) {
        free(res->items[i].path);
        free(res->items[i].value);
    }
    free(res->items);
    free(res->errmsg);
    memset(res, 0, sizeof(*res));
}

void cm_free_string(char *s) {
    free(s);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type extractedLeaf struct {
	Path  string
	Value string
	IsKey bool
}

func extractLeaves(yangDir, payload string) ([]extractedLeaf, error) {
	cYangDir := C.CString(yangDir)
	defer C.free(unsafe.Pointer(cYangDir))
	cPayload := C.CString(payload)
	defer C.free(unsafe.Pointer(cPayload))

	var res C.cm_result
	if C.cm_extract_leafs(cYangDir, cPayload, &res) != 0 {
		defer C.cm_result_free(&res)
		if res.errmsg != nil {
			return nil, fmt.Errorf(C.GoString(res.errmsg))
		}
		return nil, fmt.Errorf("failed to extract YANG leaves")
	}
	defer C.cm_result_free(&res)

	count := int(res.count)
	if count == 0 {
		return nil, nil
	}
	items := unsafe.Slice(res.items, count)
	out := make([]extractedLeaf, 0, count)
	for _, item := range items {
		out = append(out, extractedLeaf{
			Path:  C.GoString(item.path),
			Value: C.GoString(item.value),
			IsKey: item.is_key != 0,
		})
	}
	return out, nil
}

func validateYANGJSON(yangDir, payload string) error {
	cYangDir := C.CString(yangDir)
	defer C.free(unsafe.Pointer(cYangDir))
	cPayload := C.CString(payload)
	defer C.free(unsafe.Pointer(cPayload))

	var errMsg *C.char
	if C.cm_validate_json(cYangDir, cPayload, &errMsg) != 0 {
		defer C.cm_free_string(errMsg)
		if errMsg != nil {
			return fmt.Errorf(C.GoString(errMsg))
		}
		return fmt.Errorf("YANG validation failed")
	}
	return nil
}
