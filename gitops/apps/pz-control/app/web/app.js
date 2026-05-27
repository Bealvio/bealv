const $ = (id) => document.getElementById(id);

async function refreshStatus() {
  try {
    const r = await fetch("/api/status");
    if (!r.ok) throw new Error("status http " + r.status);
    const s = await r.json();
    const badge = $("status-badge");
    badge.textContent = s.phase + (s.ready ? " · ready" : "");
    badge.className = "badge " + (s.ready ? "ok" : (s.phase === "Running" ? "warn" : "bad"));
    $("pod-name").textContent = s.name || "—";
    $("started-at").textContent = s.started_at ? new Date(s.started_at).toLocaleString() : "—";
    $("restarts").textContent = s.restarts ?? "—";
  } catch (e) {
    $("status-badge").textContent = "error";
    $("status-badge").className = "badge bad";
  }
}

async function refreshLogs() {
  try {
    const r = await fetch("/api/logs");
    const txt = await r.text();
    $("logs-pre").textContent = txt;
    $("logs-pre").scrollTop = $("logs-pre").scrollHeight;
  } catch (e) {
    $("logs-pre").textContent = "Impossible de récupérer les logs: " + e;
  }
}

async function doRestart() {
  if (!confirm("Redémarrer le serveur ? Tous les joueurs connectés seront déconnectés.")) return;
  const btn = $("restart-btn");
  const msg = $("restart-msg");
  btn.disabled = true;
  msg.textContent = "Redémarrage en cours…";
  msg.className = "";
  try {
    const r = await fetch("/api/restart", { method: "POST" });
    if (r.ok) {
      msg.textContent = "Restart déclenché.";
      msg.className = "ok";
    } else {
      const t = await r.text();
      msg.textContent = "Erreur: " + t;
      msg.className = "error";
    }
  } catch (e) {
    msg.textContent = "Erreur réseau: " + e;
    msg.className = "error";
  } finally {
    setTimeout(() => { btn.disabled = false; }, 5000);
  }
}

$("restart-btn").addEventListener("click", doRestart);
refreshStatus();
refreshLogs();
setInterval(refreshStatus, 5000);
setInterval(refreshLogs, 5000);
