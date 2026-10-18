// Session controls stay visible even when noVNC is showing an empty desktop.
const bar = document.createElement("nav");
bar.id = "session-controls";
bar.setAttribute("aria-label", "Console session");
bar.innerHTML = `
  <a href="/">Connection screen</a>
  <span id="session-message" role="status" aria-live="polite"></span>
  <button type="button">Disconnect / reconnect</button>
`;
document.body.append(bar);
const button = bar.querySelector("button");
const message = bar.querySelector("[role=status]");
let disconnecting = false;

async function request(path, options = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 12000);
  try {
    const response = await fetch(path, {
      cache: "no-store", ...options, signal: controller.signal,
    });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return await response.json();
  } finally {
    clearTimeout(timer);
  }
}

button.addEventListener("click", async () => {
  if (disconnecting) return;
  disconnecting = true;
  button.disabled = true;
  message.textContent = "Disconnecting…";
  try {
    await request("/api/disconnect", {method: "POST"});
    location.replace("/");
  } catch (error) {
    message.textContent = "Could not disconnect. Try again or open the connection screen.";
    disconnecting = false;
    button.disabled = false;
  }
});

async function poll() {
  try {
    const status = await request("/api/status");
    // Only a confirmed ended session redirects; an outage keeps the recovery
    // controls visible. Do not race a disconnect request already in flight.
    if (disconnecting) return;
    if (["idle", "stopped", "failed"].includes(status.state)) {
      location.replace("/");
      return;
    }
    message.textContent = status.state === "starting" ? "Connecting…" : "";
  } catch (error) {
    if (!disconnecting) message.textContent = "Connection lost — retrying…";
  } finally {
    setTimeout(poll, 2000);
  }
}
poll();
