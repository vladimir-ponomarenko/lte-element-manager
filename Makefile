LIBYANG_REPO        := https://github.com/CESNET/libyang.git
LIBNETCONF2_REPO    := https://github.com/CESNET/libnetconf2.git

LIBYANG_REF         ?= v4.2.2
LIBNETCONF2_REF     ?= v4.1.2

THIRD_PARTY_DIR     := $(CURDIR)/third_party
BUILD_DIR           := $(CURDIR)/.build
PREFIX_DIR          := $(CURDIR)/.local

LIBYANG_SRC         := $(THIRD_PARTY_DIR)/libyang
LIBNETCONF2_SRC     := $(THIRD_PARTY_DIR)/libnetconf2

LIBYANG_BUILD       := $(BUILD_DIR)/libyang
LIBNETCONF2_BUILD   := $(BUILD_DIR)/libnetconf2

CMAKE               ?= cmake
GIT                 ?= git
GO                  ?= go

LIBYANG_CMAKE_FLAGS := \
	-DCMAKE_BUILD_TYPE=Release \
	-DCMAKE_INSTALL_PREFIX=$(PREFIX_DIR)

LIBNETCONF2_CMAKE_FLAGS := \
	-DCMAKE_BUILD_TYPE=Release \
	-DCMAKE_INSTALL_PREFIX=$(PREFIX_DIR) \
	-DLIBYANG_INCLUDE_DIR=$(PREFIX_DIR)/include \
	-DLIBYANG_LIBRARY=$(PREFIX_DIR)/lib/libyang.so

.PHONY: all bootstrap deps clone clone-libyang clone-libnetconf2 \
	update-libyang update-libnetconf2 libyang libnetconf2 clean distclean \
	build build-netconf build-netconf-server netconf-client run test cover

all: libyang libnetconf2


build:
	$(GO) build -o ems-agent ./cmd/ems-agent
	$(MAKE) build-netconf-server

test:
	CGO_ENABLED=0 $(GO) test ./...

cover:
	@mkdir -p .artifacts
	CGO_ENABLED=0 $(GO) test ./... -covermode=atomic -coverprofile=.artifacts/coverage.out
	$(GO) tool cover -func=.artifacts/coverage.out | tee .artifacts/coverage.txt
	$(GO) tool cover -html=.artifacts/coverage.out -o .artifacts/coverage.html
	@awk 'BEGIN { FS="[: ,]+" } NR==1 { next } { file=$$1; stmts=$$(NF-1); count=$$NF; total[file]+=stmts; if (count>0) covered[file]+=stmts } END { for (f in total) { pct=(total[f]>0)?(100*covered[f]/total[f]):0; printf "%.2f\t%d\t%d\t%s\n", pct, covered[f], total[f], f } }' .artifacts/coverage.out | sort -n > .artifacts/coverage_by_file.tsv
	@awk 'BEGIN { FS="\t" } { printf "%6.2f%% %5d/%5d %s\n", $$1, $$2, $$3, $$4 }' .artifacts/coverage_by_file.tsv > .artifacts/coverage_by_file.txt
	@echo ""
	@echo "Lowest-covered files (statements):"
	@head -n 25 .artifacts/coverage_by_file.txt
	@echo ""
	@echo "Open: .artifacts/coverage.html"

build-netconf: libnetconf2 libyang
	CGO_ENABLED=1 LD_LIBRARY_PATH=$(PREFIX_DIR)/lib $(GO) build -tags netconf -o ems-agent ./cmd/ems-agent

build-netconf-server: libnetconf2 libyang
	$(CC) -O2 -o netconf-server ./cmd/netconf-server/server.c \
		-I$(PREFIX_DIR)/include -L$(PREFIX_DIR)/lib -lnetconf2 -lyang -lssh -lssl -lcrypto -lcurl -lpthread

run: build
	./ems-agent

bootstrap: clone libnetconf2 libyang

deps:
	@mkdir -p $(THIRD_PARTY_DIR) $(BUILD_DIR) $(PREFIX_DIR)

clone: clone-libyang clone-libnetconf2

clone-libyang: deps
	@if [ ! -d "$(LIBYANG_SRC)/.git" ]; then \
		$(GIT) clone --depth 1 --branch "$(LIBYANG_REF)" "$(LIBYANG_REPO)" "$(LIBYANG_SRC)"; \
	else \
		echo "libyang already cloned: $(LIBYANG_SRC)"; \
	fi

clone-libnetconf2: deps
	@if [ ! -d "$(LIBNETCONF2_SRC)/.git" ]; then \
		$(GIT) clone --depth 1 --branch "$(LIBNETCONF2_REF)" "$(LIBNETCONF2_REPO)" "$(LIBNETCONF2_SRC)"; \
	else \
		echo "libnetconf2 already cloned: $(LIBNETCONF2_SRC)"; \
	fi

update-libyang:
	@test -d "$(LIBYANG_SRC)/.git" || (echo "Missing git repo: $(LIBYANG_SRC)"; exit 1)
	cd "$(LIBYANG_SRC)" && \
		$(GIT) fetch origin "$(LIBYANG_REF)" && \
		$(GIT) checkout "$(LIBYANG_REF)" && \
		$(GIT) pull --ff-only origin "$(LIBYANG_REF)"

update-libnetconf2:
	@test -d "$(LIBNETCONF2_SRC)/.git" || (echo "Missing git repo: $(LIBNETCONF2_SRC)"; exit 1)
	cd "$(LIBNETCONF2_SRC)" && \
		$(GIT) fetch origin "$(LIBNETCONF2_REF)" && \
		$(GIT) checkout "$(LIBNETCONF2_REF)" && \
		$(GIT) pull --ff-only origin "$(LIBNETCONF2_REF)"

$(LIBYANG_BUILD)/CMakeCache.txt: clone-libyang
	mkdir -p "$(LIBYANG_BUILD)"
	$(CMAKE) -S "$(LIBYANG_SRC)" -B "$(LIBYANG_BUILD)" $(LIBYANG_CMAKE_FLAGS)

$(LIBNETCONF2_BUILD)/CMakeCache.txt: $(LIBYANG_BUILD)/CMakeCache.txt clone-libnetconf2
	mkdir -p "$(LIBNETCONF2_BUILD)"
	$(CMAKE) -S "$(LIBNETCONF2_SRC)" -B "$(LIBNETCONF2_BUILD)" $(LIBNETCONF2_CMAKE_FLAGS)

libyang: $(LIBYANG_BUILD)/CMakeCache.txt
	$(CMAKE) --build "$(LIBYANG_BUILD)" --parallel
	$(CMAKE) --install "$(LIBYANG_BUILD)"

libnetconf2: libyang $(LIBNETCONF2_BUILD)/CMakeCache.txt
	$(CMAKE) --build "$(LIBNETCONF2_BUILD)" --parallel
	$(CMAKE) --install "$(LIBNETCONF2_BUILD)"

clean:
	rm -rf $(BUILD_DIR)
	rm -rf .artifacts
	rm -f ems-agent netconf-server $(PREFIX_DIR)/bin/netconf-client

distclean: clean
	rm -rf $(THIRD_PARTY_DIR) $(PREFIX_DIR)

# ? Build and install NETCONF client for tests.
netconf-client: libnetconf2
	@mkdir -p "$(PREFIX_DIR)/bin"
	$(CC) -O2 -o "$(PREFIX_DIR)/bin/netconf-client" ./cmd/netconf-client/netconf-client.c \
		-I"$(PREFIX_DIR)/include" -L"$(PREFIX_DIR)/lib" -lnetconf2 -lyang -lssh -lssl -lcrypto -lpthread
