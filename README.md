# OldIPMI — ancient Java iKVM consoles in a modern browser

Supermicro/AMI MegaRAC BMCs from the 2010s ship their remote console as a Java Web Start
app (`jviewer.jnlp`). No current browser runs applets, no current JRE ships `javaws`, and
the JARs are signed with certificates that expired years ago. This puts the viewer in a
container and hands you the console over plain HTTPS-able noVNC instead.

```
browser ──http/ws──> noVNC ──> websockify ──> x11vnc ──> Xvfb ──> JViewer.jar (Java 8)
                                                                       │
                                                  AMI KVM protocol (5901 video, 5900 HID,
                                                       5120/5123 virtual media)
                                                                       ▼
                                                                      BMC
```

## Why not just point noVNC at the BMC

Because ports 5900/5901 do not speak RFB. They wait for the client to send an AMI
proprietary handshake carrying a session token, and 5120/5123 are AMI's `IUSB` virtual
media protocol. Verified against the hardware — there is no VNC server to talk to, so the
Java client genuinely has to run somewhere.

## Why not `javaws` / IcedTea-Web / OpenWebStart

`JViewer.jar` is signed with `SMCCERT.RSA` from 2016. Every Web Start implementation
validates that chain and refuses. `java -cp` ignores JAR signatures entirely, so we read
`main-class` from the JNLP (defaulting to `com.ami.kvm.jviewer.JViewer`) and invoke it directly with the
arguments the JNLP would have passed. No trust-store surgery.

## Setup

```bash
cp .env.example .env      # optional: fill in BMC_USER / BMC_PASS
./start.sh
```

The launcher builds and starts the container in the background, waits for the connect
page, and opens <http://127.0.0.1:8088/> in your default browser. It works from any
working directory and reuses the container on subsequent runs when nothing changed.
Podman, `podman-compose` and `curl` are required; Linux browser opening uses `xdg-open`.
Run as your normal user for rootless containers. Existing `.env` settings are loaded
by podman-compose. No Docker daemon or Podman API socket is needed.

The launcher calls `podman-compose` directly: `podman compose` can otherwise prefer
an installed Docker Compose provider ([Podman documentation](https://docs.podman.io/en/latest/markdown/podman-compose.1.html)).
The existing `Dockerfile` and `docker-compose.yml` filenames are supported by Podman
and stay in place. The image is built locally as `localhost/oldipmi-kvm`; the base
image's fully qualified name avoids interactive registry selection. `.containerignore`
keeps `.env`, JNLP tokens and Python caches out of the build context.

Only TCP port `8088` on loopback is published (to container port `8080`); the page,
API and console WebSocket share it. BMC connections are outbound and need no extra
published ports.

Use `./start.sh --no-browser` on a headless machine, or `./start.sh SERVICE` for
another service added to the Compose file. To stop it, run
`podman-compose stop lab-ami` from this directory. Inspect logs with
`podman-compose logs lab-ami`. A rebuilt image replaces the container on the next
launch; an unchanged image and configuration reuse it to preserve the active session.

If you previously ran this app with Docker, stop its container first with
`docker compose stop lab-ami` to free port 8088. Podman uses separate container and
image storage, so the first Podman launch rebuilds the image; `.env` can stay as-is.

## Two ways in

`/` is a connect page offering both routes:

- **Drop a `.jnlp`.** Drag the file the BMC's web UI gave you onto the page. It goes
  straight down the launcher's stdin and never touches the container's filesystem —
  the tokens inside are single-use, so a temp file would only be a liability.
- **Type host, user and password.** The container logs in itself and mints a fresh
  JNLP, which is the only way to get a token that is not already spent.

Either way the console lands at `/vnc.html`, and the page is where you go back to
switch servers or reconnect. Credentials in `.env` are optional: set all three and the
container connects at boot exactly as it always did; leave them empty and it comes up
on the page instead of refusing to start.

The browser console defaults to zoom to fit (noVNC's **Local Scaling**), including
direct visits to `/vnc.html`. It scales with the browser window; the noVNC settings
still let you choose another scaling mode.

The console has an always-visible **Disconnect / reconnect** button. It stops the
current Java viewer and returns to the connection screen, where you can upload a
fresh JNLP or log in again. **Connection screen** returns there without stopping
the viewer. If the session ends or the container restarts, the console automatically
returns to the connection screen instead of leaving an empty black desktop.

Failures now show up in the browser rather than in `podman logs` — a rejected password
comes back as the BMC's own `HAPI_STATUS`, a role below Operator as its `CURPRIV`, and
firmware that hides its JNLP somewhere unusual says so.

### More servers

Copy the `lab-ami:` block in `docker-compose.yml`, change the name, `BMC_HOST` and the host
port. One container per BMC session — see the session limit below.

## Tokens expire — this is the whole reason for the login step

A downloaded `.jnlp` is good for exactly one launch. Reuse it and JViewer shows:

> Authentication failed. Token issued for this session is expired.

So the container logs in itself on every start:

1. `POST /rpc/WEBSES/create.asp` with `WEBVAR_USERNAME` / `WEBVAR_PASSWORD`
2. read `SESSION_COOKIE` out of the JavaScript response (it is not JSON), check `HAPI_STATUS == 0`
3. `GET /Java/jviewer.jnlp` with that cookie — confirmed from the BMC's own `impl/remote_control_imp.js`
4. parse `codebase`, the `<jar>` list, the Linux x86_64 `<nativelib>` and the 12 `<argument>`s
5. download the JARs from the BMC, extract the `.so` files, exec `java`

### The JNLP fetch lies about its length

This firmware answers `/Java/jviewer.jnlp` with `Content-length: 3757`, sends 2034 bytes
of complete, valid XML, and closes. urllib3 correctly calls that a broken stream and
discards the body — and its `IncompleteRead` carries a byte *count*, not the bytes, so
there is nothing to recover at the `requests` layer.

The symptom is misleading: the fetch of the *correct* path raises, `fetch_jnlp` moves on
to the remaining candidates, they all 404, and you get told to go find the right path in
the web UI. The path was right all along.

So `fetch_jnlp` retries a truncated response through `http.client`, which hands back what
actually arrived, and says so in the log:

```
[jviewer]   /Java/jviewer.jnlp -> server cut the body short; using the 2034B it sent
[jviewer] got JNLP from /Java/jviewer.jnlp
```

Only the JNLP endpoint does this. The JAR downloads are well-formed and stay on
`requests`.

If your firmware serves the JNLP from a path not in `JNLP_CANDIDATES`, pass it explicitly:

```bash
podman-compose run --rm --service-ports lab-ami --jnlp-path '/cgi/url_redirect.cgi?url_name=man_ikvm&url_type=jwsk'
```

To find that path on another box, you do not need to log in at all. This firmware gates
HTML pages behind a session but serves `/impl/*.js` and `/lib/*.js` unauthenticated, and it
answers *missing* files under `/impl/` with a clean `404` while every gated path returns
`405 Access Denied`. So `/impl/` is a reliable existence oracle:

```bash
# find the console page's implementation file
for n in remote_control console_redirection jviewer ikvm kvm_console; do
  curl -so /dev/null -w "%{http_code} /impl/${n}_imp.js\n" "http://$BMC/impl/${n}_imp.js"
done
# then just read it -- the launch URL is in plain sight
curl -s "http://$BMC/impl/remote_control_imp.js" | grep -i 'window.open'
```

On the box this was built against that prints
`window.open('/Java/jviewer.jnlp','RemoteConsole',...)`.

**Privileges.** The same file shows the BMC requires `CURPRIV` of Administrator (4) or
Operator (3) to launch the console; `jviewer-launch` checks `/rpc/getrole.asp` and warns
rather than failing opaquely.

### One-off with a JNLP you already downloaded

Dropping it on the page is usually easier, but the flag still works and still connects
at boot:

```bash
podman run --rm -p 127.0.0.1:8088:8080 \
  -v ~/Downloads/jviewer.jnlp:/tmp/j.jnlp:ro,Z localhost/oldipmi-kvm --jnlp /tmp/j.jnlp
```

Works once, then the token is burnt — but the container now stays up on the connect
page afterwards instead of exiting, so you can drop a fresh one without restarting it.

### ATEN viewers and Pack200 downloads

ATEN/Supermicro `launch.jnlp` files are also supported through the upload route.
The launcher honors their `application-desc main-class`, such as
`tw.com.aten.ikvm.KVMMain`; the credential-login route remains AMI-specific.
When the JNLP sets `jnlp.packEnabled=true`, both application and native JARs are fetched
as `.jar.pack.gz` and unpacked with Java 8's `unpack200`. A missing packed resource
(HTTP 404/410) falls back to the plain JAR, as in Java Web Start.

Some BMCs serve only the packed files: a 404 for `iKVM__V1.69.21.0x0.jar` does not
mean its `.jar.pack.gz` counterpart is missing. The original JNLP needs no editing.
All application arguments are redacted from launcher logs because token positions
differ between viewers. `--dump-only` deliberately prints the full command, including
tokens; do not share its output.

Run the launcher regression checks with `python3 -m unittest discover -s tests -v`.

## Operational notes

**Legacy HTTPS.** BMC certificate verification is disabled for login, JNLP fetches,
and application/native JAR downloads (including Pack200). Expired, self-signed,
untrusted and hostname-mismatched certificates do not block the launcher.

**Session limits.** MegaRAC firmware allows only a few concurrent iKVM sessions.
`kvm-session` traps SIGTERM and kills the Java process so its TCP connections close, and
it kills the previous session before starting a new one, but *whether that actually frees
the BMC's session slot immediately is untested here* — JViewer has no clean logout hook,
so the BMC may hold the slot until its own timeout either way. Assume a killed session
can block the next launch for a few minutes, which now includes reconnecting from the
page. If the iKVM wedges,
port 623 is open and `ipmitool -I lanplus -H <bmc> -U <user> -P <pass> mc reset cold`
resets the BMC without touching the host OS.

**Container lifetime.** The container used to exit when JViewer did. It now stays up and
returns you to the connect page, because reconnecting without a restart is the point of
the page. `restart: "no"` still means a crash does not loop.

**Mouse.** Set Mouse Mode to *Absolute* in the BMC's own web UI. In relative mode the
browser pointer and the BMC's pointer drift apart permanently and no container-side setting
fixes it.

**Keyboard.** The chain browser → noVNC keysym → x11vnc → Xvfb → Java `KeyEvent` → BMC HID
has five places to mangle modifiers. It is wired US/EN end to end (`x11vnc -xkb`, JNLP arg
11 = `EN`). Keep the layout identical at every hop; for anything exotic use JViewer's own
Video → Soft Keyboard, which bypasses the whole chain.

**Exposure.** The compose file binds `127.0.0.1` deliberately. This is an unauthenticated
route to a server console *and its power controls*, and since the connect page landed it
is also where BMC credentials get typed and JNLPs get uploaded. Reach it through an SSH
tunnel (`ssh -L 8088:127.0.0.1:8088 containerhost`) or put an authenticating proxy in front.

Setting `WEB_PASSWORD` turns on HTTP basic auth (user `kvm`) across the whole surface —
the page, the API, noVNC and the WebSocket — which is worth doing now that the port
accepts credentials. It is off by default because it breaks a bookmarked
`?autoconnect=1` link. `VNC_PASSWORD` adds an x11vnc password underneath it. Neither
encrypts anything; the tunnel does that.

Worth knowing before you widen the binding: a JNLP names a `codebase`, and the launcher
downloads JARs from it and runs them. Uploading a JNLP is therefore choosing a host to
fetch and execute code from. That is fine for a file your own BMC just handed you, and
it is the reason this port should stay behind the tunnel.

## Files

| path | what |
|---|---|
| `Dockerfile` | Podman build: Temurin 8 JRE + Xvfb/x11vnc/fluxbox + noVNC |
| `docker-compose.yml` | podman-compose service, environment and loopback port binding |
| `.containerignore` | excludes credentials, JNLPs and caches from the Podman build context |
| `start.sh` | builds/starts the container, waits for HTTP, opens the browser |
| `bin/entrypoint.sh` | brings up X and x11vnc, then the two services below |
| `bin/kvm-web` | port 8080: connect page, control API, noVNC, WebSocket — websockify with two extra routes |
| `bin/kvm-session` | owns the JViewer process so it can be started and restarted from the page |
| `bin/jviewer-launch` | login, JNLP fetch/parse, JAR download, `java` exec |
| `web/index.html` | the connect page, served as noVNC's `index.html` |
| `fluxbox/init` | strips toolbar and window decoration so only the console shows |

`fbsetbg` is replaced with a no-op in the image. fluxbox calls it on startup whatever the
style says, and with no wallpaper setter installed it answers by parking a 636x136
`xmessage` in the middle of the screen — on top of the console. The root window is
painted by `rootCommand` instead, which previously called an `xsetroot` that is not in
this image and so did nothing at all.

`kvm-session` is a separate process on purpose. websockify hands every connection to a
fresh interpreter — Python 3.14 defaults `multiprocessing` to `forkserver` — so nothing
that has to outlive a single request can live in the web layer. The same quirk is why
both scripts guard their startup behind `if __name__ == "__main__":`; without it the
re-imported module rebinds the port and every request dies with a connection reset.
