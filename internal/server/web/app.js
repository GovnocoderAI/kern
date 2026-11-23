// Kern web UI — dependency-free client for the SSE server.
(() => {
  const token = new URLSearchParams(location.search).get("token") || "";
  const $ = (id) => document.getElementById(id);

  const chat = $("chat");
  const input = $("input");
  const sendBtn = $("send");
  const modelSel = $("model");
  const agentSel = $("agent");
  const sessionsUl = $("sessions");
  const permModal = $("perm");
  const toast = $("toast");

  let pendingPermission = null;
  let busy = false;
  let currentAssistant = null;

  function api(path, opts = {}) {
    const headers = Object.assign({ "Content-Type": "application/json" }, opts.headers || {});
    if (token) headers["Authorization"] = "Bearer " + token;
    return fetch(path, Object.assign({}, opts, { headers }));
  }

  function showToast(text) {
    toast.textContent = text;
    toast.classList.remove("hidden");
    setTimeout(() => toast.classList.add("hidden"), 4000);
  }

  function el(tag, cls, text) {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  function addMessage(role, text) {
    const wrap = el("div", "msg " + role);
    wrap.appendChild(el("div", "role", role === "user" ? "you" : "kern"));
    const body = el("div", "body", text || "");
    wrap.appendChild(body);
    chat.appendChild(wrap);
    chat.scrollTop = chat.scrollHeight;
    return body;
  }

  function addTool(name, summary) {
    const n = el("div", "tool");
    n.appendChild(el("div", "title", "▸ " + name + (summary ? " " + summary : "")));
    chat.appendChild(n);
    chat.scrollTop = chat.scrollHeight;
    return n;
  }

  function finishTool(name, ok, output) {
    const tools = [...chat.querySelectorAll(".tool")];
    for (let i = tools.length - 1; i >= 0; i--) {
      if (tools[i].dataset.done) continue;
      const t = tools[i];
      t.dataset.done = "1";
      t.classList.add(ok ? "ok" : "err");
      t.querySelector(".title").textContent = (ok ? "✓ " : "✗ ") + name;
      if (output) t.appendChild(el("div", "out", output));
      break;
    }
    chat.scrollTop = chat.scrollHeight;
  }

  async function loadConfig() {
    const res = await api("/api/config");
    if (!res.ok) { showToast("config load failed"); return; }
    const cfg = await res.json();
    $("workdir").textContent = cfg.workdir || "";
    modelSel.innerHTML = "";
    (cfg.providers || []).forEach((p) => {
      (p.models || []).forEach((m) => {
        const ref = p.id + "/" + m;
        const opt = el("option", null, ref);
        opt.value = ref;
        if (p.id === cfg.provider && m === cfg.model) opt.selected = true;
        modelSel.appendChild(opt);
      });
      if (!p.models || p.models.length === 0) {
        const opt = el("option", null, p.id + "/" + cfg.model);
        opt.value = p.id + "/" + cfg.model;
        if (p.id === cfg.provider) opt.selected = true;
        modelSel.appendChild(opt);
      }
    });
    // Roles
    agentSel.innerHTML = "";
    agentSel.appendChild(el("option", null, "orchestrator"));
    agentSel.firstChild.value = "";
    (cfg.agents || []).forEach((a) => {
      const opt = el("option", null, a);
      opt.value = a;
      agentSel.appendChild(opt);
    });
  }

  async function loadSessions() {
    const res = await api("/api/sessions");
    if (!res.ok) return;
    const data = await res.json();
    sessionsUl.innerHTML = "";
    (data.sessions || []).forEach((s) => {
      const li = el("li", s.active ? "active" : "", s.title || s.id);
      li.title = s.model + " · " + s.id;
      li.onclick = () => loadSession(s.id);
      sessionsUl.appendChild(li);
    });
  }

  async function loadSession(id) {
    if (busy) return;
    const res = await api("/api/sessions/load", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    if (!res.ok) { showToast("load failed"); return; }
    chat.innerHTML = "";
    pendingPermission = null;
    permModal.classList.add("hidden");
    await loadSessions();
    const ss = await (await api("/api/sessions")).json();
    const found = (ss.sessions || []).find((x) => x.id === id);
    if (found) addMessage("assistant", found.title ? "" : "");
    showToast("loaded " + (found ? found.title : id));
  }

  async function newSession() {
    if (busy) return;
    const res = await api("/api/sessions/new", { method: "POST" });
    if (!res.ok) { showToast("new session failed"); return; }
    chat.innerHTML = "";
    await loadSessions();
  }

  function handleEvent(event, data) {
    if (event === "text") {
      if (!currentAssistant) currentAssistant = addMessage("assistant", "");
      currentAssistant.textContent += data.delta || "";
      chat.scrollTop = chat.scrollHeight;
    } else if (event === "tool_start") {
      currentAssistant = null;
      addTool(data.name, data.summary);
    } else if (event === "tool_result") {
      finishTool(data.name, data.ok, data.output);
    } else if (event === "permission") {
      showPermission(data);
    } else if (event === "turn_done") {
      if (data && data.error) {
        addMessage("assistant", "error: " + data.error);
        showToast(data.error);
      }
      currentAssistant = null;
      busy = false;
      setBusy(false);
      loadSessions();
    }
  }

  function showPermission(data) {
    pendingPermission = data.id;
    $("perm-action").textContent = data.action;
    $("perm-resource").textContent = data.resource;
    permModal.classList.remove("hidden");
  }

  async function replyPermission(allow, always) {
    if (!pendingPermission) return;
    permModal.classList.add("hidden");
    try {
      await api("/api/permission", {
        method: "POST",
        body: JSON.stringify({ id: pendingPermission, allow, always }),
      });
    } catch (_) {}
    pendingPermission = null;
  }

  function setBusy(b) {
    busy = b;
    sendBtn.disabled = b;
    sendBtn.textContent = b ? "…" : "Send";
  }

  async function send() {
    const text = input.value.trim();
    if (!text || busy) return;
    input.value = "";
    addMessage("user", text);
    currentAssistant = null;
    setBusy(true);

    const body = { text };
    // The role selector runs the turn as that role by re-targeting on the
    // server is out of scope for v0.2; we pass nothing and let the server use
    // its configured agent. (Kept in the UI for the upcoming role switch.)
    try {
      const res = await api("/api/chat", { method: "POST", body: JSON.stringify(body) });
      if (!res.ok || !res.body) {
        showToast("chat failed: " + res.status);
        setBusy(false);
        return;
      }
      await readSSE(res.body);
    } catch (e) {
      showToast(String(e));
      setBusy(false);
    }
  }

  async function readSSE(stream) {
    const reader = stream.getReader();
    const decoder = new TextDecoder();
    let buf = "";
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      let idx;
      while ((idx = buf.indexOf("\n\n")) >= 0) {
        const chunk = buf.slice(0, idx);
        buf = buf.slice(idx + 2);
        let event = "message";
        const dataLines = [];
        chunk.split("\n").forEach((line) => {
          if (line.startsWith("event:")) event = line.slice(6).trim();
          else if (line.startsWith("data:")) dataLines.push(line.slice(5).trimStart());
        });
        const raw = dataLines.join("\n");
        let data = null;
        if (raw) { try { data = JSON.parse(raw); } catch (_) { data = raw; } }
        handleEvent(event, data);
      }
    }
  }

  // Wire up
  sendBtn.onclick = send;
  $("newSession").onclick = newSession;
  $("perm-allow").onclick = () => replyPermission(true, false);
  $("perm-always").onclick = () => replyPermission(true, true);
  $("perm-deny").onclick = () => replyPermission(false, false);
  modelSel.onchange = async () => {
    await api("/api/model", { method: "POST", body: JSON.stringify({ model: modelSel.value }) });
    showToast("model: " + modelSel.value);
  };
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  });

  loadConfig();
  loadSessions();
})();
