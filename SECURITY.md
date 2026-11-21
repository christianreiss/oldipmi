# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's
[private vulnerability reporting](https://github.com/christianreiss/oldipmi/security/advisories/new),
not in a public issue. Name the OldIPMI version, the console family and what
you observed, and leave out real addresses, account names and passwords.
Fixes go into the next release.

## The security model in short

* **Passwords** are kept in KWallet, or only for the session. They never appear
  in profiles, logs, the window or the agent interface, and they reach the
  backend process over a private socket pair, never on a command line.
* **Transport:** TLS certificates are verified by default. HTTP login, old or
  unverified TLS and plain console or media transport are separate exceptions,
  off by default and enabled per connection. A connection that the policy
  refuses sends no credentials.
* **AI agents:** while the agent interface is on (the default), any program
  running as your user can drive the open consoles through
  `$XDG_RUNTIME_DIR/oldipmi/agent.sock`, power control and ISO mounting
  included, without asking. That is intended. Other users cannot reach the
  socket, and `oldipmi-mcp` refuses one served by another user. Switch it off
  in Settings → AI agents, or start OldIPMI with `--no-agent-api`.
* **The BMCs themselves** are old and often have known vulnerabilities that
  will never be fixed. Keep their management interfaces on a separate network.
