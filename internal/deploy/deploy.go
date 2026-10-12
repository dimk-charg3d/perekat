// Package deploy installs the relay server on a remote machine over SSH.
package deploy

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/charg3d/perekat/internal/relay"
)

// The server programs are built by `make relay` before anything that embeds them.
var (
	//go:embed bin/perekat-relay-linux-amd64
	relayAMD64 []byte
	//go:embed bin/perekat-relay-linux-arm64
	relayARM64 []byte
)

// Target is the machine to install the relay server on.
type Target struct {
	Host string `json:"host"`
	// Port is the SSH port; zero means 22.
	Port int `json:"port"`
	// User is the SSH login; empty means root.
	User     string `json:"user"`
	Password string `json:"password"`
}

const installScript = `set -e
command -v systemctl >/dev/null || { echo "this server has no systemd" >&2; exit 1; }
# Nothing is changed if the port belongs to another program.
if ! systemctl is-active --quiet perekat-relay && command -v ss >/dev/null 2>&1 && ss -Hltn "sport = :@PORT@" | grep -q .; then
  echo "TCP port @PORT@ on the server is already used by another program" >&2
  rm -f /tmp/perekat-relay.upload
  exit 1
fi
install -m 755 /tmp/perekat-relay.upload /usr/local/bin/perekat-relay
rm -f /tmp/perekat-relay.upload
cat > /etc/systemd/system/perekat-relay.service <<'UNIT'
[Unit]
Description=Perekat relay server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/perekat-relay serve -dir /var/lib/perekat-relay -port @PORT@
DynamicUser=yes
StateDirectory=perekat-relay
Restart=always
RestartSec=3
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable perekat-relay >/dev/null 2>&1
systemctl restart perekat-relay
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
  ufw allow @PORT@/tcp >/dev/null
fi
# The port is opened in the running firewall and in its saved configuration
# separately: reloading the firewall would drop the rules Docker has added.
if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  firewall-cmd --add-port=@PORT@/tcp >/dev/null
  firewall-cmd --permanent --add-port=@PORT@/tcp >/dev/null
fi
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if /usr/local/bin/perekat-relay info -dir /var/lib/perekat-relay 2>/dev/null; then exit 0; fi
  sleep 1
done
echo "the relay server did not start; check: journalctl -u perekat-relay" >&2
exit 1
`

// Install puts the relay server on the target, starts it as a service and
// returns what devices need to connect to it. Running it again on a server
// that already has a relay updates the program and keeps its key, so devices
// that joined earlier stay connected. progress is told what is going on.
func Install(ctx context.Context, t Target, progress func(string)) (relay.Config, error) {
	if t.Port == 0 {
		t.Port = 22
	}
	if t.User == "" {
		t.User = "root"
	}

	progress("Подключаюсь к серверу")
	client, err := dial(ctx, t)
	if err != nil {
		return relay.Config{}, fmt.Errorf("cannot log in to the server: %w", err)
	}
	defer client.Close()
	s := remote{client: client, target: t}

	arch, err := s.run("uname -m", nil)
	if err != nil {
		return relay.Config{}, err
	}
	var program []byte
	switch strings.TrimSpace(arch) {
	case "x86_64", "amd64":
		program = relayAMD64
	case "aarch64", "arm64":
		program = relayARM64
	default:
		return relay.Config{}, fmt.Errorf("unsupported server architecture %q", strings.TrimSpace(arch))
	}

	progress("Копирую программу сервера")
	if _, err := s.run("cat > /tmp/perekat-relay.upload", bytes.NewReader(program)); err != nil {
		return relay.Config{}, err
	}

	progress("Устанавливаю и запускаю")
	script := strings.ReplaceAll(installScript, "@PORT@", strconv.Itoa(relay.DefaultPort))
	out, err := s.root(script)
	if err != nil {
		return relay.Config{}, err
	}
	var cfg relay.Config
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return relay.Config{}, fmt.Errorf("unexpected answer from the server: %q", out)
	}
	cfg.Host = t.Host
	return cfg, nil
}

func dial(ctx context.Context, t Target) (*ssh.Client, error) {
	var auth []ssh.AuthMethod
	if t.Password != "" {
		auth = append(auth,
			ssh.Password(t.Password),
			// Some servers ask for the password this way instead.
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = t.Password
				}
				return answers, nil
			}))
	}
	home, _ := os.UserHomeDir()
	var signers []ssh.Signer
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		if pem, err := os.ReadFile(filepath.Join(home, ".ssh", name)); err == nil {
			if signer, err := ssh.ParsePrivateKey(pem); err == nil {
				signers = append(signers, signer)
			}
		}
	}
	if len(signers) > 0 {
		auth = append(auth, ssh.PublicKeys(signers...))
	}

	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{
		User:            t.User,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback(filepath.Join(home, ".ssh", "known_hosts")),
		Timeout:         15 * time.Second,
	})
	if err != nil {
		conn.Close()
		return nil, err
	}
	return ssh.NewClient(cc, chans, reqs), nil
}

// hostKeyCallback refuses a server whose key differs from the one recorded in
// known_hosts, and accepts a server seen for the first time, as there is
// nothing to compare its key with.
func hostKeyCallback(knownHostsPath string) ssh.HostKeyCallback {
	check, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return ssh.InsecureIgnoreHostKey()
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			return nil
		}
		return err
	}
}

type remote struct {
	client *ssh.Client
	target Target
}

// run executes a command and returns what it printed.
func (r remote) run(command string, stdin io.Reader) (string, error) {
	session, err := r.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdin, session.Stdout, session.Stderr = stdin, &stdout, &stderr
	if err := session.Run(command); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return stdout.String(), nil
}

// root runs a shell script with administrator rights.
func (r remote) root(script string) (string, error) {
	if r.target.User == "root" {
		return r.run("sh -s", strings.NewReader(script))
	}
	quoted := "'" + strings.ReplaceAll(script, "'", `'\''`) + "'"
	return r.run("sudo -S -p '' sh -c "+quoted, strings.NewReader(r.target.Password+"\n"))
}

// ParseTarget reads [user@]host[:port].
func ParseTarget(s string) (Target, error) {
	t := Target{User: "root", Port: 22}
	if user, rest, ok := strings.Cut(s, "@"); ok {
		t.User, s = user, rest
	}
	t.Host = s
	if host, port, ok := strings.Cut(s, ":"); ok {
		n, err := strconv.Atoi(port)
		if err != nil {
			return t, fmt.Errorf("bad port %q", port)
		}
		t.Host, t.Port = host, n
	}
	if t.Host == "" || t.User == "" {
		return t, errors.New("expected [user@]host[:port]")
	}
	return t, nil
}
