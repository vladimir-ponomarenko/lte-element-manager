package netconf

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	emserrors "lte-element-manager/internal/errors"
)

// ProcessServer wraps the libnetconf2 SSH server binary.
type ProcessServer struct {
	Binary        string
	Addr          string
	YangDir       string
	SnapshotPath  string
	ControlURL    string
	HostKey       string
	AuthorizedKey string
	Username      string
	Log           zerolog.Logger
}

var execCommand = exec.Command
var processKillTimeout = 5 * time.Second

func (p *ProcessServer) Name() string {
	return "netconf-server"
}

func (p *ProcessServer) Run(ctx context.Context) error {
	if p.Binary == "" {
		return emserrors.New(emserrors.ErrCodeConfig, "netconf binary is empty",
			emserrors.WithOp("netconf"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
	args := []string{
		"-addr", p.Addr,
		"-yang", p.YangDir,
		"-snapshot", p.SnapshotPath,
		"-hostkey", p.HostKey,
		"-authorized-key", p.AuthorizedKey,
		"-user", p.Username,
	}
	if strings.TrimSpace(p.ControlURL) != "" {
		args = append(args, "-control", strings.TrimSpace(p.ControlURL))
	}

	cmd := execCommand(p.Binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return emserrors.Wrap(err, emserrors.ErrCodeProcess, "attach stdout pipe failed",
			emserrors.WithOp("netconf"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return emserrors.Wrap(err, emserrors.ErrCodeProcess, "attach stderr pipe failed",
			emserrors.WithOp("netconf"),
			emserrors.WithSeverity(emserrors.SeverityMajor),
		)
	}

	if err := cmd.Start(); err != nil {
		return emserrors.Wrap(err, emserrors.ErrCodeProcess, "start netconf process failed",
			emserrors.WithOp("netconf"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}

	p.Log.Info().Str("addr", p.Addr).Msg("netconf ssh server started")
	p.Log.Debug().Int("pid", cmd.Process.Pid).Msg("netconf process started")

	done := make(chan error, 1)
	go func() {
		scanNetconfOutput(stdout, p.SnapshotPath, p.Log)
	}()
	go func() {
		scanNetconfErrors(stderr, p.Log)
	}()
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		p.Log.Debug().Msg("netconf stopping (context canceled)")
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-time.After(processKillTimeout):
			p.Log.Debug().Msg("netconf kill (grace timeout)")
			_ = cmd.Process.Kill()
		case <-done:
		}
		return nil
	case err := <-done:
		return emserrors.Wrap(err, emserrors.ErrCodeProcess, "netconf process exited",
			emserrors.WithOp("netconf"),
			emserrors.WithSeverity(emserrors.SeverityCritical),
		)
	}
}

func scanNetconfOutput(r io.Reader, snapshotPath string, log zerolog.Logger) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 256*1024)
	// NETCONF_GET lines may include small JSON payloads. Keep scanner limit generous.
	scanner.Buffer(buf, 64*1024*1024)
	debug := log.GetLevel() <= zerolog.DebugLevel
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "NETCONF_GET ") {
			emitNetconfGetLog(line, snapshotPath, log, debug)
			continue
		}
		if debug {
			log.Debug().Msg(line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Warn().Err(err).Msg("netconf stdout scan failed")
	}
}

func scanNetconfErrors(r io.Reader, log zerolog.Logger) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)
	debug := log.GetLevel() <= zerolog.DebugLevel
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "[ERR]") || strings.Contains(line, "ERROR") || strings.Contains(line, "error") {
			log.Error().Msg(line)
			continue
		}
		if debug {
			log.Debug().Msg(line)
		}
	}
	if err := scanner.Err(); err != nil {
		if debug {
			log.Debug().Err(err).Msg("netconf stderr scan failed")
			return
		}
		log.Warn().Err(err).Msg("netconf stderr scan failed")
	}
}

func emitNetconfGetLog(line, snapshotPath string, log zerolog.Logger, debug bool) {
	rest := strings.TrimPrefix(line, "NETCONF_GET ")
	parts := strings.SplitN(rest, " json=", 2)
	meta := strings.Fields(parts[0])
	var user, ts string
	var bytesStr, sha string
	for _, f := range meta {
		if strings.HasPrefix(f, "user=") {
			user = strings.TrimPrefix(f, "user=")
		} else if strings.HasPrefix(f, "ts=") {
			ts = strings.TrimPrefix(f, "ts=")
		} else if strings.HasPrefix(f, "bytes=") {
			bytesStr = strings.TrimPrefix(f, "bytes=")
		} else if strings.HasPrefix(f, "sha256=") {
			sha = strings.TrimPrefix(f, "sha256=")
		}
	}

	e := log.Info().
		Str("user", user).
		Str("ts", ts).
		Str("bytes", bytesStr).
		Str("sha256", sha)

	// Only log payload in debug.
	// If C server included json=... keep it. Otherwise, read snapshot from file to show actual served state.
	if debug {
		if len(parts) == 2 {
			e = e.RawJSON("metrics", []byte(parts[1]))
		} else if snapshotPath != "" {
			if b, err := os.ReadFile(snapshotPath); err == nil {
				e = e.RawJSON("metrics", b)
			} else {
				e = e.Err(err)
			}
		}
	}
	e.Msg("netconf_get")
}
