package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aenix-io/aeman/internal/server"
)

// `aeman service` runs the MCP daemon under the platform's user-level
// service manager, so the board is there before the first client asks for
// it, and comes back whenever the session it belongs to does: launchd
// starts a LaunchAgent at login, and a systemd user unit needs `loginctl
// enable-linger` to outlive a logout at all. The unit is the whole
// contract: the
// manager hands the daemon none of the environment the install was typed
// in, and the file itself is readable by anything running as this user.

const (
	// serviceLabel is the launchd label and reverse-DNS name of the agent;
	// systemdUnitName the unit systemd knows it by.
	serviceLabel    = "io.aenix.aeman"
	systemdUnitName = "aeman.service"
	// defaultListenAddr is where the daemon listens when nothing says
	// otherwise; the same address every client is then pointed at.
	defaultListenAddr = "127.0.0.1:8766"
	// healthUnknown is what an answer on the port that aeman did not write
	// is called: the port is open, the daemon is not necessarily there.
	healthUnknown = "unknown"
	// stopSlack is the headroom the manager's deadline keeps over the stop
	// the daemon actually performs, so the kill lands after the last push
	// has had its full budget rather than during it.
	stopSlack = 5 * time.Second
)

// stopTimeout is the manager's patience before it kills the daemon, in
// seconds: the daemon's own stop plus headroom. Derived rather than written
// down, because launchd sends SIGKILL 20 seconds after SIGTERM by default —
// less than the drain alone — and a number that drifts behind the budgets
// destroys the commits the queue was holding.
func stopTimeout() int {
	return int((mcpStopBudget() + stopSlack).Round(time.Second).Seconds())
}

// errNoServiceOnWindows: there is no user-level service manager here to
// install into, so the refusal names the command that does the same by hand.
var errNoServiceOnWindows = errors.New("aeman service is not supported on Windows: run `aeman mcp --listen 127.0.0.1:8766` yourself")

// unit is what a service manager is asked to run: a binary, its arguments,
// the environment it would otherwise not have, and where its output goes.
type unit struct {
	Label   string
	Bin     string
	Args    []string
	Env     map[string]string
	LogPath string
}

// serviceUnit builds the unit for a resolved board. bin must be absolute.
func serviceUnit(bin, addr string, insecure bool, cfg *server.GitConfig, env func(string) string, logPath string) unit {
	args := []string{"mcp", "--listen", addr}
	if insecure {
		args = append(args, "--listen-insecure")
	}
	args = append(args, flagArgs(cfg)...)
	u := unit{Label: serviceLabel, Bin: bin, Args: args, Env: map[string]string{}, LogPath: logPath}
	// launchd starts an agent with a minimal PATH that has neither
	// Homebrew nor ~/go/bin on it, and the daemon reads its credential
	// from the forge's CLI — so without this it comes up and then cannot
	// find `gh`.
	if p := env("PATH"); p != "" {
		u.Env["PATH"] = p
	}
	// PATH gets the daemon to the right `gh`; these get that `gh` to the
	// right config, which is where its token is. Durable paths, unlike the
	// agent socket, so carrying them is safe: a stale one would be a
	// directory that moved, not a session that ended.
	for _, k := range []string{"GH_CONFIG_DIR", "GLAB_CONFIG_DIR", "XDG_CONFIG_HOME"} {
		if v := env(k); v != "" {
			u.Env[k] = v
		}
	}
	// The board's days live in one zone for every user; a unit inherits
	// nothing, so the setting travels with it.
	if tz := env("AEMAN_TZ"); tz != "" {
		u.Env["AEMAN_TZ"] = tz
	}
	return u
}

// envKeys is the unit's environment in a fixed order, so reinstalling the
// same configuration produces the same file.
func (u unit) envKeys() []string {
	keys := make([]string, 0, len(u.Env))
	for k := range u.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// launchdPlist renders the LaunchAgent.
func launchdPlist(u unit) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	fmt.Fprintf(&b, "\t<key>Label</key>\n\t<string>%s</string>\n", xmlText(u.Label))
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range append([]string{u.Bin}, u.Args...) {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", xmlText(a))
	}
	b.WriteString("\t</array>\n")
	if len(u.Env) > 0 {
		b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
		for _, k := range u.envKeys() {
			fmt.Fprintf(&b, "\t\t<key>%s</key>\n\t\t<string>%s</string>\n", xmlText(k), xmlText(u.Env[k]))
		}
		b.WriteString("\t</dict>\n")
	}
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	// Background is a trade: the daemon's git work stays out of the
	// foreground's way, and the tool calls somebody is waiting for pay for
	// it with throttled CPU and I/O. Adaptive is not a way out —
	// launchd.plist(5) moves an Adaptive job between the tiers on XPC
	// transaction activity, and this one speaks HTTP.
	b.WriteString("\t<key>ProcessType</key>\n\t<string>Background</string>\n")
	if u.LogPath != "" {
		fmt.Fprintf(&b, "\t<key>StandardOutPath</key>\n\t<string>%s</string>\n", xmlText(u.LogPath))
		fmt.Fprintf(&b, "\t<key>StandardErrorPath</key>\n\t<string>%s</string>\n", xmlText(u.LogPath))
	}
	fmt.Fprintf(&b, "\t<key>ExitTimeOut</key>\n\t<integer>%d</integer>\n", stopTimeout())
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// systemdUnit renders the user unit.
func systemdUnit(u unit) string {
	var b strings.Builder
	b.WriteString("[Unit]\nDescription=aeman MCP daemon\nAfter=network.target\n")
	// Restart=always at a 5s interval never trips systemd's own default
	// limit, so a daemon that dies at every start — a --data another
	// process holds, a port already taken — would restart for as long as
	// the machine is up. launchd has no equivalent and throttles only.
	fmt.Fprintf(&b, "StartLimitIntervalSec=%d\nStartLimitBurst=5\n\n", 120)
	b.WriteString("[Service]\nType=simple\n")
	cmd := make([]string, 0, len(u.Args)+1)
	for _, a := range append([]string{u.Bin}, u.Args...) {
		cmd = append(cmd, systemdArg(a))
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", strings.Join(cmd, " "))
	for _, k := range u.envKeys() {
		fmt.Fprintf(&b, "Environment=\"%s=%s\"\n", k, systemdValue(u.Env[k]))
	}
	b.WriteString("Restart=always\nRestartSec=5\n")
	fmt.Fprintf(&b, "TimeoutStopSec=%d\n\n", stopTimeout())
	b.WriteString("[Install]\nWantedBy=default.target\n")
	return b.String()
}

func xmlText(s string) string {
	var b bytes.Buffer
	// EscapeText only fails on a write error, and a bytes.Buffer has none;
	// the branch that returned "" for it could only ever have produced a
	// plist with an empty Label.
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// systemdValue escapes an Environment= value: systemd expands %X specifiers
// there, so a literal percent has to be doubled. Variables are not expanded
// in Environment=, so a dollar stands for itself.
func systemdValue(s string) string {
	// A newline for the same reason systemdArg escapes one: an
	// Environment= line is a line, and a value carrying a newline splits it
	// into something systemd will not parse. The values reaching here are
	// no longer only PATH.
	return strings.NewReplacer("%", "%%", `\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`).Replace(s)
}

// systemdArg is one ExecStart argument, quoted when it would otherwise
// split — the committer identity carries a space, and paths can. Quoting is
// not enough by itself: systemd expands $VAR in a command line after quote
// removal, so a literal dollar has to be doubled the way a percent does.
func systemdArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\r\"'\\%$;") {
		return s
	}
	// A newline cannot be quoted into a systemd command line at all — the
	// unit is line-oriented — so it is escaped rather than carried through.
	return `"` + strings.NewReplacer("%", "%%", "$", "$$", `\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`).Replace(s) + `"`
}

// addrFromUnit reads back the address an installed unit runs the daemon on,
// so status need not be told what install already recorded. The plist wraps
// each argument in an element and the systemd unit puts them on one line;
// dropping the wrapper leaves the same token stream.
func addrFromUnit(content string) string {
	fields := strings.Fields(strings.NewReplacer("<string>", " ", "</string>", " ").Replace(content))
	for i, f := range fields {
		if f == "--listen" && i+1 < len(fields) {
			return strings.Trim(fields[i+1], `"`)
		}
	}
	return ""
}

// serviceMgr talks to the platform's user-level service manager. goos is a
// field rather than runtime.GOOS directly so both units are exercised
// wherever the tests run, and run is the subprocess seam.
type serviceMgr struct {
	goos, home, uid string
	run             func(ctx context.Context, name string, args ...string) (string, error)
}

// openServiceMgr is how install, uninstall and status get their manager.
// A variable, so one test can drive `runService` itself: without a seam the
// only way to exercise the real entry point would be to let it write into
// the developer's own home and call launchctl.
var openServiceMgr = newServiceMgr

func newServiceMgr() (*serviceMgr, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home directory: %w", err)
	}
	return &serviceMgr{goos: runtime.GOOS, home: home, uid: strconv.Itoa(os.Getuid()), run: runCommand}, nil
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // the service manager's own command line, built here
	return string(out), err
}

// unitPath is where the manager reads the unit from.
func (m *serviceMgr) unitPath() string {
	if m.goos == "darwin" {
		return filepath.Join(m.home, "Library", "LaunchAgents", serviceLabel+".plist")
	}
	return filepath.Join(m.home, ".config", "systemd", "user", systemdUnitName)
}

// logPath is where the daemon's output ends up: a file launchd is told to
// write, or the journal systemd keeps on its own.
func (m *serviceMgr) logPath() string {
	if m.goos == "darwin" {
		return filepath.Join(m.home, "Library", "Logs", "aeman", "aeman.log")
	}
	return ""
}

// logsHint is what to read to see what the daemon said.
func (m *serviceMgr) logsHint() string {
	if p := m.logPath(); p != "" {
		return p
	}
	// Repeated failed starts trip StartLimitBurst and leave the unit
	// permanently failed; nothing but this command or a reboot clears it,
	// and `manager: not loaded` on its own does not say so.
	return "journalctl --user --unit " + systemdUnitName +
		" (and `systemctl --user reset-failed " + systemdUnitName + "` if it gave up after repeated failures)"
}

// refuseIfInstalled is the "already there" refusal. Split out so install can
// give it BEFORE the data-directory check: the daemon it would replace is
// holding that directory, so checking the directory first answers an upgrade
// — the ordinary case — with a message written for a second client.
func (m *serviceMgr) refuseIfInstalled() error {
	if m.goos == "windows" {
		return errNoServiceOnWindows
	}
	path := m.unitPath()
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists: run `aeman service uninstall` first", path)
	}
	return nil
}

// prepare is every refusal an install owes before it writes anything, in the
// order that matters.
func (m *serviceMgr) prepare(dataDir, addr string) error {
	if err := m.refuseIfInstalled(); err != nil {
		return err
	}
	// The directory before the port: a daemon of this same board holds
	// both, and the directory is what names that case. Told about the port
	// instead, the operator would go looking for a second address rather
	// than find the daemon they already have.
	if err := dataDirAvailable(dataDir); err != nil {
		return err
	}
	return portAvailable(addr)
}

// portAvailable refuses an address something already holds, before the unit
// file is written. Advisory exactly as dataDirAvailable is: the port can be
// taken in the moment between this and the daemon's own bind, and the point
// is not to make that impossible but to catch the case that is otherwise
// silent. `/healthz` names no board, so another aeman answering here passes
// for this one: awaitHealthy accepts it, the install prints success and the
// address, and every client that follows that line writes into the other
// board.
func portAvailable(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("--listen %s is not free (%w): a unit written now would fail its bind at every start while clients were pointed at whatever does answer there, so a second board on this machine needs a port of its own (--listen host:port)", addr, err)
	}
	return ln.Close()
}

// dataDirAvailable refuses when something else already holds the directory.
// Only a directory that exists can be held, and creating one here would
// leave a stray behind on every refused typo.
func dataDirAvailable(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		// Nothing can be holding a directory that is not there yet; a
		// directory that cannot be read at all is its own problem and has
		// to say so rather than pass as free.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("data dir: %w", err)
	}
	held, err := server.DataDirHold(dir)
	if err != nil {
		return err
	}
	return held.Close()
}

// install writes the unit and hands it to the manager. It refuses to write
// over one that is there: the manager would keep running the old command
// line while the file said otherwise.
func (m *serviceMgr) install(ctx context.Context, u unit) error {
	if err := m.refuseIfInstalled(); err != nil {
		return err
	}
	path := m.unitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("unit directory: %w", err)
	}
	if m.goos == "darwin" {
		if u.LogPath != "" {
			if err := os.MkdirAll(filepath.Dir(u.LogPath), 0o750); err != nil {
				return fmt.Errorf("log directory: %w", err)
			}
		}
		if err := os.WriteFile(path, []byte(launchdPlist(u)), 0o600); err != nil {
			return err
		}
		// gui/<uid> is the login session: an agent, not a system daemon. A
		// root daemon would read the System keychain instead of the login
		// keychain the person's credential lives in.
		if out, err := m.run(ctx, "launchctl", "bootstrap", "gui/"+m.uid, path); err != nil {
			// A unit the manager would not start must not survive the
			// attempt: the next install would be refused by a file that
			// runs nothing.
			_ = os.Remove(path)
			return fmt.Errorf("launchctl bootstrap: %w: %s", err, strings.TrimSpace(out))
		}
		return nil
	}
	if err := os.WriteFile(path, []byte(systemdUnit(u)), 0o600); err != nil {
		return err
	}
	// systemd does not see a unit file until it is told to look, and
	// enabling one it has not read fails.
	if out, err := m.run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		m.discard(ctx, path)
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(out))
	}
	if out, err := m.run(ctx, "systemctl", "--user", "enable", "--now", systemdUnitName); err != nil {
		m.discard(ctx, path)
		return fmt.Errorf("systemctl enable: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// discard takes back a unit the manager refused, so the next install is not
// stopped by a file that starts nothing.
func (m *serviceMgr) discard(ctx context.Context, path string) {
	// disable first: an `enable --now` that created the wants symlink and
	// then failed to start leaves it behind, and removing only the unit
	// file makes the next daemon-reload complain about a dangling link.
	_, _ = m.run(ctx, "systemctl", "--user", "disable", systemdUnitName)
	_ = os.Remove(path)
	_, _ = m.run(ctx, "systemctl", "--user", "daemon-reload")
}

// uninstall stops the daemon and removes its unit. Nothing installed is the
// state it exists to reach, not a failure. The returned string is what the
// caller has to pass on rather than swallow: the manager did not confirm
// the stop.
func (m *serviceMgr) uninstall(ctx context.Context) (string, error) {
	if m.goos == "windows" {
		return "", errNoServiceOnWindows
	}
	path := m.unitPath()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	// Stop first: a unit file removed under a running daemon leaves it
	// holding the data directory with nothing left to stop it by.
	var out string
	var stopErr error
	if m.goos == "darwin" {
		out, stopErr = m.run(ctx, "launchctl", "bootout", "gui/"+m.uid+"/"+serviceLabel)
		// bootout exits non-zero both for a label that was never loaded and
		// for a launchd that cannot be asked at all. Only the first is
		// harmless, so the stop is forgiven only when launchd positively
		// says it does not know the label.
		if stopErr != nil && m.labelIsAbsent(ctx) {
			out, stopErr = "", nil
		}
	} else {
		out, stopErr = m.run(ctx, "systemctl", "--user", "disable", "--now", systemdUnitName)
	}
	// The stop fails harmlessly when nothing was loaded, and really when
	// there is no user bus to ask (a non-lingering SSH session). This cannot
	// tell those apart, so removing the unit either way is right — claiming
	// the daemon stopped is not, because the second case leaves it running
	// and holding the data directory with the unit already gone.
	var warn string
	if stopErr != nil {
		warn = fmt.Sprintf("the service manager did not confirm the stop (%v)", stopErr)
		if detail := strings.TrimSpace(out); detail != "" {
			warn += ": " + detail
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return warn, err
	}
	if m.goos != "darwin" {
		_, _ = m.run(ctx, "systemctl", "--user", "daemon-reload")
	}
	return warn, nil
}

// manualStop is the command that stops a daemon the service manager would
// not. Two states need it: a stop the manager did not confirm, and a unit
// file already gone while the process may not be. In both, the operator is
// left holding a data directory with nothing in the tool to release it.
func (m *serviceMgr) manualStop() string {
	if m.goos == "darwin" {
		return "launchctl bootout gui/" + m.uid + "/" + serviceLabel
	}
	return "systemctl --user stop " + systemdUnitName
}

// labelIsAbsent reports whether launchd says, unmistakably, that it does not
// know the label. Anything else — including a launchd that cannot be reached
// — answers false, because the warning it suppresses is the one that says
// the daemon may still be running with its unit already removed. Erring
// towards a warning about a daemon that is already gone is the cheap
// direction.
func (m *serviceMgr) labelIsAbsent(ctx context.Context) bool {
	out, err := m.run(ctx, "launchctl", "print", "gui/"+m.uid+"/"+serviceLabel)
	if err == nil {
		return false // launchd still has it
	}
	return strings.Contains(out, "Could not find service")
}

// svcStatus is three independent answers. A unit file on disk does not mean
// the manager loaded it, and a loaded unit does not mean the process behind
// it can serve.
type svcStatus struct {
	Installed bool
	UnitPath  string
	Running   bool
	Healthy   bool
	// Health is what the daemon called itself; "degraded" means it is
	// answering but its commits are not reaching the remote.
	Health string
	Addr   string
	Logs   string
}

// well reports whether the daemon is running and saying so.
func (s svcStatus) well() bool {
	return s.Running && s.Healthy && (s.Health == "" || s.Health == "ok")
}

func (s svcStatus) String() string {
	var b strings.Builder
	if s.Installed {
		fmt.Fprintf(&b, "unit:    installed at %s\n", s.UnitPath)
	} else {
		b.WriteString("unit:    none (run `aeman service install --repo name=url`)\n")
	}
	// "loaded", not "running": launchctl print exits 0 for any job it has,
	// including one it is throttling after repeated crashes, so a
	// crashlooping daemon under KeepAlive would otherwise read as running
	// beside a health line that says nothing answered.
	if s.Running {
		b.WriteString("manager: loaded\n")
	} else {
		b.WriteString("manager: not loaded\n")
	}
	switch {
	case !s.Healthy:
		fmt.Fprintf(&b, "health:  no answer at http://%s/healthz\n", s.Addr)
	case s.Health != "" && s.Health != "ok":
		fmt.Fprintf(&b, "health:  %s at http://%s/healthz\n", s.Health, s.Addr)
	default:
		fmt.Fprintf(&b, "health:  ok at http://%s/healthz\n", s.Addr)
	}
	// Whenever it is not simply well: running-but-degraded and
	// running-but-silent are exactly when the logs hold the answer.
	if s.Installed && !s.well() && s.Logs != "" {
		fmt.Fprintf(&b, "logs:    %s\n", s.Logs)
	}
	return b.String()
}

// status asks all three questions. addr overrides the address the unit
// records, for a daemon someone is running by hand.
func (m *serviceMgr) status(ctx context.Context, addr string) (svcStatus, error) {
	if m.goos == "windows" {
		return svcStatus{}, errNoServiceOnWindows
	}
	st := svcStatus{UnitPath: m.unitPath(), Addr: addr, Logs: m.logsHint()}
	content, err := os.ReadFile(st.UnitPath) //nolint:gosec // the path this manager owns
	switch {
	case err == nil:
		st.Installed = true
	case errors.Is(err, os.ErrNotExist):
	default:
		return st, fmt.Errorf("read %s: %w", st.UnitPath, err)
	}
	if st.Addr == "" {
		st.Addr = addrFromUnit(string(content))
	}
	if st.Addr == "" {
		st.Addr = defaultListenAddr
	}
	if m.goos == "darwin" {
		_, err = m.run(ctx, "launchctl", "print", "gui/"+m.uid+"/"+serviceLabel)
	} else {
		_, err = m.run(ctx, "systemctl", "--user", "is-active", systemdUnitName)
	}
	st.Running = err == nil
	st.Healthy, st.Health = healthzAnswers(ctx, st.Addr)
	return st, nil
}

// awaitHealthy waits for the daemon the manager has just been handed to
// answer. `install` otherwise prints success over a unit that dies at every
// start and is respawned for as long as the machine is up — a busy port, a
// bad repository, anything the address check cannot see.
func (m *serviceMgr) awaitHealthy(ctx context.Context, addr string) bool {
	for {
		// "unknown" is something answering on the port that is not aeman —
		// a squatter, which is one of the cases this exists to catch, so it
		// must not count as the daemon having come up.
		if ok, status := healthzAnswers(ctx, addr); ok && status != healthUnknown {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// healthProbe asks after the daemon without following redirects: whatever
// is on that port may not be aeman, and a squatter that answers with a
// redirect must not be able to send the probe somewhere else.
var healthProbe = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// healthzAnswers asks the daemon itself, because a manager reporting a unit
// as loaded says nothing about whether the process behind it can serve. It
// returns whether it answered and what it called itself: a daemon whose
// pushes are failing answers, and says degraded.
func healthzAnswers(ctx context.Context, addr string) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// The address is the operator's own --listen, or the one read back from
	// the unit they installed: nothing untrusted reaches it. `status` does
	// not run it past checkListenAddr, so this will GET whatever host was
	// asked for — which is the operator asking, not an exposure.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil) //nolint:gosec // G704: see above
	if err != nil {
		return false, ""
	}
	resp, err := healthProbe.Do(req) //nolint:gosec // G704: the same request, the same reason
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	// Whatever is on that port may not be aeman, and it must not be able to
	// make `status` read an unbounded body.
	body := io.LimitReader(resp.Body, 64<<10)
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, body)
		return false, ""
	}
	var got struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(body).Decode(&got); err != nil || got.Status == "" {
		// Something is answering on the port, but not with anything this
		// understands — which is not the same as a daemon saying it is
		// well, so it must not be reported as one.
		return true, healthUnknown
	}
	return true, got.Status
}

// stableBinaryPath is the path to write into a unit. resolved is what
// os.Executable returned, which on Linux is /proc/self/exe read through —
// the real file, not the name it was reached by. A version-managed install
// (/usr/local/bin/aeman -> /opt/aeman-1.2.3/aeman) would then bake the
// versioned path into the unit, and the next upgrade leaves it failing
// 203/EXEC with Restart=always retrying forever. The name the command was
// invoked by is the one that survives an upgrade, so it wins — but only
// while it still names this very file, or an older aeman earlier on PATH
// would be installed in place of the one running.
func stableBinaryPath(argv0, resolved string) string {
	invoked, err := exec.LookPath(argv0)
	if err != nil {
		return resolved
	}
	if invoked, err = filepath.Abs(invoked); err != nil {
		return resolved
	}
	ri, err := os.Stat(resolved)
	if err != nil {
		return resolved
	}
	ii, err := os.Stat(invoked)
	if err != nil || !os.SameFile(ri, ii) {
		return resolved
	}
	return invoked
}

// runService is `aeman service install|uninstall|status`.
func runService(args []string) error {
	if len(args) == 0 {
		return errors.New("aeman service needs a verb: install, uninstall or status")
	}
	verb := args[0]
	if verb == "help" || strings.HasPrefix(verb, "-") {
		fmt.Print(`aeman service - run the MCP daemon under the platform's service manager

Usage:
  aeman service install [flags]   Write the unit and start the daemon
  aeman service uninstall         Stop the daemon and remove the unit
  aeman service status [flags]    Unit, manager and daemon, asked separately

Flags: --listen host:port (env AEMAN_MCP_LISTEN), --listen-insecure, and the
storage flags of 'aeman mcp', whose resolved values are baked into the unit.
`)
		return nil
	}
	fs := flag.NewFlagSet("service "+verb, flag.ContinueOnError)
	listen := fs.String("listen", os.Getenv("AEMAN_MCP_LISTEN"),
		"address the daemon listens on (env AEMAN_MCP_LISTEN; default "+defaultListenAddr+")")
	listenInsecure := fs.Bool("listen-insecure", false, "let --listen bind a non-loopback address, exposing the board to whoever can reach it")
	gf := addGitFlags(fs, os.Getenv)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	ctx := context.Background()
	switch verb {
	case "install":
		return installService(ctx, *listen, *listenInsecure, gf)
	case "uninstall":
		m, err := openServiceMgr()
		if err != nil {
			return err
		}
		// Whether a unit file was there decides what this run may claim
		// afterwards: without one, uninstall asks the manager nothing, so
		// it learns nothing about the process and cannot call it stopped.
		_, statErr := os.Stat(m.unitPath())
		hadUnit := statErr == nil
		warn, err := m.uninstall(ctx)
		if err != nil {
			return err
		}
		switch {
		case warn != "":
			fmt.Printf("warning: %s\n", warn)
			fmt.Printf("the unit is removed, but the daemon may still be running and holding its data directory; `%s` is what stops it\n", m.manualStop())
		case !hadUnit:
			fmt.Printf("no unit file is installed; if a daemon from an earlier install still holds the data directory, `%s` is what stops it\n", m.manualStop())
		default:
			fmt.Println("aeman is no longer installed as a service")
		}
		return nil
	case "status":
		m, err := openServiceMgr()
		if err != nil {
			return err
		}
		// Only a --listen typed on this command line outranks the unit:
		// the unit's address is what the installed daemon binds, and an
		// AEMAN_MCP_LISTEN left in the shell would otherwise send status
		// to a port nothing is on and report a live daemon as silent.
		asked := ""
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "listen" {
				asked = f.Value.String()
			}
		})
		st, err := m.status(ctx, asked)
		if err != nil {
			return err
		}
		fmt.Print(st.String())
		return nil
	default:
		return fmt.Errorf("unknown service verb %q (install, uninstall or status)", verb)
	}
}

// installTarget is everything an install has to agree on before anything on
// the machine is touched: an address that keeps the board off the network,
// and a board to serve. Separate from installService so those refusals can
// be tested without a service manager going anywhere near a real home
// directory — a test that reached one would install a LaunchAgent on
// whoever ran it.
func installTarget(listen string, insecure bool, gf *gitFlags) (string, *server.GitConfig, error) {
	addr := listen
	if addr == "" {
		addr = defaultListenAddr
	}
	if err := checkListenAddr(addr, insecure); err != nil {
		return "", nil, err
	}
	// Port 0 is fine for a run somebody is watching — the kernel picks and
	// the log says which — but a unit baked with it starts a daemon on a
	// port nothing can be pointed at.
	// Both errors are dropped: checkListenAddr above has already refused an
	// address that does not split and a port that is not a number, so the
	// only way to reach zero here is a port spelled zero.
	_, port, _ := net.SplitHostPort(addr)
	if n, _ := strconv.Atoi(port); n == 0 {
		return "", nil, fmt.Errorf("--listen %s: a service needs a fixed port, since nothing could be pointed at the one the kernel picks", addr)
	}
	cfg, err := gf.config()
	if err != nil {
		return "", nil, err
	}
	if cfg == nil {
		return "", nil, errors.New("aeman service install needs the board's repository: --repo name=url (or AEMAN_REPOS)")
	}
	return addr, cfg, nil
}

// installService writes the unit and hands it to the manager.
func installService(ctx context.Context, listen string, insecure bool, gf *gitFlags) error {
	m, err := openServiceMgr()
	if err != nil {
		return err
	}
	// The platform refusal comes first: on a system where the command
	// cannot work at all, "needs the board's repository" would send the
	// operator to fix a flag instead of telling them that.
	if m.goos == "windows" {
		return errNoServiceOnWindows
	}
	addr, cfg, err := installTarget(listen, insecure, gf)
	if err != nil {
		return err
	}
	// A unit installed over a directory something else holds starts, dies on
	// the lock, and is respawned for as long as the machine is up — while
	// the install that caused it printed success.
	if err := m.prepare(cfg.DataDir, addr); err != nil {
		return err
	}
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("this binary's path: %w", err)
	}
	bin = stableBinaryPath(os.Args[0], bin)
	if err := m.install(ctx, serviceUnit(bin, addr, insecure, cfg, os.Getenv, m.logPath())); err != nil {
		return err
	}
	fmt.Printf("installed %s\n", m.unitPath())
	// The unit is a plain file and carries no credential, so say where the
	// daemon will look for one rather than leave it to be discovered when
	// nothing pushes.
	fmt.Println("the daemon finds its credential the way `aeman mcp` does: its own environment, else the keychain `aeman login` writes, else the forge's CLI")
	// Printed after the unit is written, which is exactly when somebody
	// without a credential reads it and goes to log in. That login lands in
	// the keychain the daemon already read: fillGitToken resolves the token
	// once at start and only a personal domain's auth is replaced later.
	fmt.Println("it resolves that once, at start, so an `aeman login` from here reaches the daemon only after `aeman service uninstall && aeman service install`")
	if ssh := sshRemotes(cfg); len(ssh) > 0 {
		fmt.Printf("warning: ssh remotes (%s) authenticate through an agent, and a unit reaches none of yours: a systemd user unit inherits no SSH_AUTH_SOCK, and a launchd agent is handed launchd's own socket rather than this shell's, so fetch and push can fail with nothing to fall back on\n",
			strings.Join(ssh, ", "))
	}
	if stranded := strandedCredentials(cfg, os.Getenv); len(stranded) > 0 {
		fmt.Printf("warning: %s cannot travel into a unit file; the daemon will push with whatever credential it finds for itself (the forge's CLI), and `aeman service status` reports degraded once a write has been waiting longer than --unpushed-warn to reach a remote\n",
			strings.Join(stranded, ", "))
	}
	// The manager accepting the unit says nothing about the daemon coming
	// up, and nobody watches this process afterwards.
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !m.awaitHealthy(wait, addr) {
		// The likely reason on a first install rather than a fault: the
		// daemon binds only after it has cloned the board, which takes
		// minutes on one with real history.
		fmt.Printf("not answering on %s yet — the first start clones the board, which can take minutes; `aeman service status` shows when it answers, and %s says why if it does not\n",
			addr, m.logsHint())
	}
	fmt.Printf("point every MCP client at it: claude mcp add --transport http aeman http://%s/mcp --scope user\n", addr)
	// The person installing this is the one who knows whether anyone else
	// has an account on the machine, and this is the moment they can act on
	// it. Loopback is shared by every account on the host.
	fmt.Println("note: nothing authenticates that endpoint, and loopback is shared by every account on this host — the daemon is for a machine whose accounts you trust")
	return nil
}

// sshRemotes names the repositories a unit could not authenticate to. An
// ssh remote is reached through an agent, and a unit reaches no agent of the
// operator's: a systemd user unit inherits no SSH_AUTH_SOCK, and a launchd
// agent is handed launchd's own socket rather than the one the installing
// shell exported. Carrying the value would be worse than naming the
// problem, because that path dies with the session it was typed in.
func sshRemotes(cfg *server.GitConfig) []string {
	var names []string
	for _, r := range cfg.Repos {
		if needsSSHAgent(r.URL) {
			names = append(names, r.Name)
		}
	}
	return names
}

// strandedCredentials names the variables this install resolved that a unit
// file cannot carry: flagArgs renders no token and the unit inherits no
// environment, so the daemon falls back to what it can find for itself, the
// forge's CLI. Two ways that bites — a board across two organisations loses
// the per-repository tokens that case exists for, and an operator who
// authenticates by token with no CLI login gets a daemon that answers and
// never pushes at all.
func strandedCredentials(cfg *server.GitConfig, env func(string) string) []string {
	var names []string
	if cfg.Token != "" {
		names = append(names, "AEMAN_GIT_TOKEN")
	}
	// The forge's own variables are read later, by fillGitToken, so they
	// are not on cfg — but they are just as absent from the unit, and an
	// operator with only GH_TOKEN exported and no CLI login is the case
	// this warning exists for. Skipped when AEMAN_GIT_TOKEN is set, because
	// fillGitToken never looks at them then and naming a variable that was
	// never going to be used reads as a second problem.
	if cfg.Token == "" {
		for _, key := range tokenEnv(cfg.Forge) {
			if env(key) != "" {
				names = append(names, key)
			}
		}
	}
	// Userinfo in a repository URL is dropped rather than written into the
	// unit's command line, which is not private. Called a credential
	// whatever it looks like: over http(s) `https://<pat>@host` is the
	// standard token form and nothing in the URL separates it from a
	// person's login, and on any other scheme this list is only reached
	// when a password is set. An operator told "username" about a PAT stops
	// looking.
	for _, r := range cfg.Repos {
		if hasCredential(r.URL) {
			names = append(names, "the credential in "+r.Name+"'s URL")
		}
	}
	// The App is a live push credential, not configuration: without it the
	// daemon falls back to the forge CLI's token, which is another identity
	// with another reach, or to nothing at all.
	if cfg.App != nil {
		names = append(names, "AEMAN_GITHUB_APP_ID")
	}
	for _, r := range cfg.Repos {
		if r.Token != "" && r.Token != cfg.Token {
			names = append(names, tokenEnvFor(r.Name))
		}
	}
	return names
}
