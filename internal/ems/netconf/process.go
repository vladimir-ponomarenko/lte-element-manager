package netconf

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

// ProcessServer wraps the libnetconf2 SSH server binary.
type ProcessServer struct {
	Binary        string
	Addr          string
	YangDir       string
	SnapshotPath  string
	HostKey       string
	AuthorizedKey string
	Username      string
	Log           zerolog.Logger
}

func (p *ProcessServer) Name() string {
	return "netconf-server"
}

func (p *ProcessServer) Run(ctx context.Context) error {
	if p.Binary == "" {
		return fmt.Errorf("netconf binary is empty")
	}
	args := []string{
		"-addr", p.Addr,
		"-yang", p.YangDir,
		"-snapshot", p.SnapshotPath,
		"-hostkey", p.HostKey,
		"-authorized-key", p.AuthorizedKey,
		"-user", p.Username,
	}

	cmd := exec.Command(p.Binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	p.Log.Info().Str("addr", p.Addr).Msg("netconf ssh server started")
	p.Log.Debug().Int("pid", cmd.Process.Pid).Msg("netconf process started")

	done := make(chan error, 1)
	go func() {
		scanNetconfOutput(stdout, p.Log)
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
		case <-time.After(5 * time.Second):
			p.Log.Debug().Msg("netconf kill (grace timeout)")
			_ = cmd.Process.Kill()
		case <-done:
		}
		return nil
	case err := <-done:
		return err
	}
}

func scanNetconfOutput(r io.Reader, log zerolog.Logger) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, 4*1024*1024)
	debug := log.GetLevel() <= zerolog.DebugLevel
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "NETCONF_GET ") {
			emitNetconfGetLog(line, log)
			continue
		}
		if debug {
			log.Debug().Msg(line)
		}
	}
	if err := scanner.Err(); err != nil {
		if debug {
			log.Debug().Err(err).Msg("netconf stdout scan failed")
		}
	}
}

func scanNetconfErrors(r io.Reader, log zerolog.Logger) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
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
		}
	}
}

func emitNetconfGetLog(line string, log zerolog.Logger) {
	rest := strings.TrimPrefix(line, "NETCONF_GET ")
	parts := strings.SplitN(rest, " json=", 2)
	if len(parts) != 2 {
		log.Debug().Msg(line)
		return
	}
	meta := strings.Fields(parts[0])
	var user, ts string
	for _, f := range meta {
		if strings.HasPrefix(f, "user=") {
			user = strings.TrimPrefix(f, "user=")
		} else if strings.HasPrefix(f, "ts=") {
			ts = strings.TrimPrefix(f, "ts=")
		}
	}
	jsonStr := parts[1]
	log.Info().
		Str("user", user).
		Str("ts", ts).
		RawJSON("metrics", []byte(jsonStr)).
		Msg("netconf_get")
}
