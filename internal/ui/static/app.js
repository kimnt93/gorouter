const $ = (id) => document.getElementById(id);
let MASTER_KEY = sessionStorage.getItem("nr_key") || "";

async function api(path, opts = {}) {
  const res = await fetch(path, {
    ...opts,
    headers: {
      "Content-Type": "application/json",
      Authorization: "Bearer " + MASTER_KEY,
      ...(opts.headers || {}),
    },
  });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = { raw: text }; }
  if (!res.ok) throw new Error((data && (data.error || data.message)) || res.status);
  return data;
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function fmtMoney(v) { return "$" + Number(v || 0).toFixed(4); }
function fmtN(v) { return Number(v || 0).toLocaleString(); }

function show(view) {
  $("login-view").classList.toggle("hidden", view !== "login");
  $("app-view").classList.toggle("hidden", view !== "app");
}

$("login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  MASTER_KEY = $("master-key").value.trim();
  try {
    await api("/admin/auth/verify", { method: "POST", body: JSON.stringify({ key: MASTER_KEY }) });
    sessionStorage.setItem("nr_key", MASTER_KEY);
    show("app");
    loadAll();
  } catch (err) {
    const el = $("login-error");
    el.textContent = "Login failed: " + err.message;
    el.classList.remove("hidden");
  }
});

$("logout").addEventListener("click", () => {
  sessionStorage.removeItem("nr_key");
  location.reload();
});

document.querySelectorAll("#tabs button").forEach((b) => {
  b.addEventListener("click", () => {
    document.querySelectorAll("#tabs button").forEach((x) => x.classList.remove("active"));
    b.classList.add("active");
    document.querySelectorAll(".tab").forEach((t) => t.classList.add("hidden"));
    $("tab-" + b.dataset.tab).classList.remove("hidden");
    refreshTab(b.dataset.tab);
  });
});

let tenantsCache = [];
let credsCache = [];
let modelsCache = [];

async function loadAll() { await refreshTab(document.querySelector("#tabs button.active").dataset.tab); }

function refreshTab(tab) {
  if (tab === "dashboard") return loadDashboard();
  if (tab === "keys") return loadKeys();
  if (tab === "credentials") return loadCreds();
  if (tab === "models") return loadModels();
  if (tab === "usage") return loadUsage();
  if (tab === "cache") return loadCache();
}

function statCard(label, value, cls = "") {
  return `<div class="stat"><div class="l">${esc(label)}</div><div class="v ${cls}">${value}</div></div>`;
}

async function loadDashboard() {
  const s = await api("/admin/usage/summary?range=24h");
  const ratio = ((s.cache_hits / Math.max(s.requests, 1)) * 100).toFixed(1);
  $("dash-cards").innerHTML =
    statCard("Requests (24h)", fmtN(s.requests)) +
    statCard("Spend (24h)", fmtMoney(s.cost_usd)) +
    statCard("Prompt tokens", fmtN(s.prompt_tokens)) +
    statCard("Completion tokens", fmtN(s.completion_tokens)) +
    statCard("Cache hit rate", ratio + "%");
  const models = Object.entries(s.by_model || {}).sort((a, b) => b[1].cost_usd - a[1].cost_usd);
  let html = "<table><thead><tr><th>Model</th><th>Requests</th><th>Cost</th><th>In tokens</th><th>Out tokens</th></tr></thead><tbody>";
  for (const [name, u] of models) {
    html += `<tr><td><code>${esc(name)}</code></td><td>${fmtN(u.requests)}</td><td>${fmtMoney(u.cost_usd)}</td><td>${fmtN(u.in_tokens)}</td><td>${fmtN(u.out_tokens)}</td></tr>`;
  }
  if (!models.length) html += '<tr><td colspan="5" class="muted">No usage yet</td></tr>';
  $("dash-models").innerHTML = html + "</tbody></table>";
}

async function loadKeys() {
  const [keys, tenants] = await Promise.all([api("/admin/api-keys"), api("/admin/tenants")]);
  tenantsCache = tenants || [];
  let html = "<table><thead><tr><th>Name</th><th>Tenant</th><th>Key</th><th>Models</th><th>Quota</th><th>RPM</th><th>Status</th><th></th></tr></thead><tbody>";
  for (const k of keys) {
    html += `<tr>
      <td>${esc(k.name)}</td>
      <td>${esc(k.tenant_name || k.tenant_id)}</td>
      <td><code>${esc(k.key_prefix)}…</code></td>
      <td title="${esc(k.models.join(", "))}">${k.models.length}</td>
      <td>${k.monthly_quota_usd == null ? "∞" : fmtMoney(k.monthly_quota_usd)}</td>
      <td>${k.rpm ?? "—"}</td>
      <td><span class="badge ${k.enabled ? "on" : "off"}">${k.enabled ? "active" : "disabled"}</span></td>
      <td>
        <button class="btn small" onclick="toggleKey('${k.id}', ${!k.enabled})">${k.enabled ? "Disable" : "Enable"}</button>
        <button class="btn small danger" onclick="delKey('${k.id}')">Delete</button>
      </td></tr>`;
  }
  if (!keys.length) html += '<tr><td colspan="8" class="muted">No API keys yet</td></tr>';
  $("keys-table").innerHTML = html + "</tbody></table>";
  $("kf-tenant").innerHTML = tenantsCache.map((t) => `<option value="${t.id}">${esc(t.name)}</option>`).join("");
}
window.toggleKey = async (id, enabled) => { await api("/admin/api-keys/" + id, { method: "PATCH", body: JSON.stringify({ enabled }) }); loadKeys(); };
window.delKey = async (id) => { if (!confirm("Delete this key? Clients using it will stop working.")) return; await api("/admin/api-keys/" + id, { method: "DELETE" }); loadKeys(); };

$("key-new").addEventListener("click", () => { $("key-revealed").classList.add("hidden"); $("key-form").classList.remove("hidden"); });
$("key-cancel").addEventListener("click", () => $("key-form").classList.add("hidden"));
$("key-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const quotaRaw = $("kf-quota").value;
  const rpmRaw = $("kf-rpm").value;
  const body = {
    tenant_id: $("kf-tenant").value,
    name: $("kf-name").value.trim(),
    models: $("kf-models").value.split(",").map((s) => s.trim()).filter(Boolean),
    monthly_quota_usd: quotaRaw === "" ? null : parseFloat(quotaRaw),
    rpm: rpmRaw === "" ? null : parseInt(rpmRaw, 10),
  };
  const created = await api("/admin/api-keys", { method: "POST", body: JSON.stringify(body) });
  $("key-form").classList.add("hidden");
  const rev = $("key-revealed");
  rev.classList.remove("hidden");
  rev.innerHTML = `<h3>Key created — copy it now</h3><p>This value is shown only once.</p><p><code style="font-size:15px">${esc(created.plaintext)}</code></p>`;
  loadKeys();
});

async function loadCreds() {
  const creds = await api("/admin/credentials");
  credsCache = creds || [];
  let html = "<table><thead><tr><th>Name</th><th>Provider</th><th>Kind</th><th>Base URL</th><th>Secret</th><th>Tenant</th><th></th></tr></thead><tbody>";
  for (const c of creds) {
    html += `<tr>
      <td>${esc(c.name)}</td><td>${esc(c.provider)}</td><td>${esc(c.kind)}</td>
      <td>${esc(c.base_url || "default")}</td><td><code>${esc(c.key_preview || "—")}</code></td>
      <td>${c.owner_tenant_id ? esc(c.owner_tenant_id) : "shared"}</td>
      <td>
        <button class="btn small" onclick="testCred('${c.id}', this)">Test</button>
        <button class="btn small danger" onclick="delCred('${c.id}')">Delete</button>
      </td></tr>`;
  }
  if (!creds.length) html += '<tr><td colspan="7" class="muted">No credentials yet</td></tr>';
  $("creds-table").innerHTML = html + "</tbody></table>";
  $("cf-owner").innerHTML = '<option value="">shared</option>' + tenantsCache.map((t) => `<option value="${t.id}">${esc(t.name)}</option>`).join("");
}
window.testCred = async (id, btn) => {
  btn.textContent = "…";
  try {
    const r = await api(`/admin/credentials/${id}/test`, { method: "POST" });
    btn.textContent = r.ok ? "OK ✓" : "Fail";
    btn.title = JSON.stringify(r).slice(0, 300);
  } catch (e) { btn.textContent = "Err"; btn.title = e.message; }
  setTimeout(() => (btn.textContent = "Test"), 2500);
};
window.delCred = async (id) => { if (!confirm("Delete credential? Routes referencing it will be removed.")) return; await api("/admin/credentials/" + id, { method: "DELETE" }); loadCreds(); };

$("cred-new").addEventListener("click", () => $("cred-form").classList.remove("hidden"));
$("cred-cancel").addEventListener("click", () => $("cred-form").classList.add("hidden"));
$("cf-kind").addEventListener("change", () => {
  const oauth = $("cf-kind").value === "oauth";
  $("cf-key-label").classList.toggle("hidden", oauth);
  $("cf-oauth-fields").classList.toggle("hidden", !oauth);
});
$("cred-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const body = {
    name: $("cf-name").value.trim(),
    provider: $("cf-provider").value,
    kind: $("cf-kind").value,
    base_url: $("cf-baseurl").value.trim(),
    owner_tenant_id: $("cf-owner").value || null,
    api_key: $("cf-apikey").value.trim(),
    oauth_access: $("cf-access").value.trim(),
    oauth_refresh: $("cf-refresh").value.trim(),
  };
  await api("/admin/credentials", { method: "POST", body: JSON.stringify(body) });
  $("cred-form").classList.add("hidden");
  e.target.reset();
  loadCreds();
});

function routeRow(sel, credId = "", priority = 0) {
  const opts = credsCache.map((c) => `<option value="${c.id}" ${c.id === credId ? "selected" : ""}>${esc(c.name)} (${esc(c.provider)})</option>`).join("");
  const div = document.createElement("div");
  div.className = "route-row";
  div.innerHTML = `<select class="rr-cred"><option value="">choose credential…</option>${opts}</select>
    <input class="rr-prio" type="number" value="${priority}" placeholder="priority">
    <button type="button" class="btn small danger" onclick="this.parentElement.remove()">✕</button>`;
  sel.appendChild(div);
}

async function loadModels() {
  const [models] = await Promise.all([api("/admin/models"), credsCache.length ? null : api("/admin/credentials").then((cs) => (credsCache = cs || []))]);
  modelsCache = models || [];
  let html = "";
  for (const m of modelsCache) {
    const routes = m.routes.map((r) => `<span class="badge">${esc(r.credential_id.slice(-6))} · p${r.priority}${r.enabled ? "" : " · off"}</span>`).join(" ");
    html += `<div class="card model-card">
      <div class="row-split">
        <h3><code>${esc(m.name)}</code> ${m.enabled ? "" : '<span class="badge off">disabled</span>'}</h3>
        <div>
          <button class="btn small" onclick='editModel(${JSON.stringify(JSON.stringify(m))})'>Edit</button>
          <button class="btn small danger" onclick="delModel('${esc(m.name)}')">Delete</button>
        </div>
      </div>
      <div class="muted">
        strategy: ${esc(m.strategy)} · upstream: <code>${esc(m.upstream_model || m.name)}</code>
        · price in/out: ${m.price ? `$${m.price.input_per_m}/$${m.price.output_per_m}` : "unpriced"}
      </div>
      <div style="margin-top:8px">${routes || '<span class="muted">no routes</span>'}</div>
    </div>`;
  }
  $("models-list").innerHTML = html || '<p class="muted">No models defined yet. Create one to expose it on /v1/chat/completions.</p>';
}
window.delModel = async (name) => { if (!confirm("Delete model " + name + "?")) return; await api("/admin/models/" + encodeURIComponent(name), { method: "DELETE" }); loadModels(); };
window.editModel = (jsonStr) => openModelForm(JSON.parse(jsonStr));

function openModelForm(existing) {
  const f = $("model-form");
  f.dataset.existing = existing ? existing.name : "";
  f.reset();
  $("mf-routes").innerHTML = "";
  if (existing) {
    $("mf-title").textContent = "Edit model: " + existing.name;
    $("mf-name").value = existing.name;
    $("mf-name").readOnly = true;
    $("mf-upstream").value = existing.upstream_model || "";
    $("mf-strategy").value = existing.strategy;
    $("mf-enabled").value = String(existing.enabled);
    if (existing.price) {
      $("mf-pin").value = existing.price.input_per_m;
      $("mf-pout").value = existing.price.output_per_m;
      $("mf-pcr").value = existing.price.cached_input_per_m;
      $("mf-pcw").value = existing.price.cache_write_per_m;
    }
    for (const r of existing.routes || []) routeRow($("mf-routes"), r.credential_id, r.priority);
  } else {
    $("mf-title").textContent = "Define model";
    $("mf-name").readOnly = false;
  }
  f.classList.remove("hidden");
}

$("model-new").addEventListener("click", () => openModelForm(null));
$("mf-add-route").addEventListener("click", () => routeRow($("mf-routes")));
$("model-cancel").addEventListener("click", () => $("model-form").classList.add("hidden"));
$("model-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const name = ($("model-form").dataset.existing || $("mf-name").value.trim());
  const routes = [...document.querySelectorAll("#mf-routes .route-row")]
    .map((row) => ({
      credential_id: row.querySelector(".rr-cred").value,
      priority: parseInt(row.querySelector(".rr-prio").value || "0", 10),
      weight: 1,
      enabled: true,
    }))
    .filter((r) => r.credential_id);
  const numOrZero = (v) => (v === "" ? 0 : parseFloat(v));
  await api("/admin/models/" + encodeURIComponent(name), {
    method: "PUT",
    body: JSON.stringify({
      strategy: $("mf-strategy").value,
      upstream_model: $("mf-upstream").value.trim(),
      enabled: $("mf-enabled").value === "true",
      routes,
    }),
  });
  await api("/admin/prices/" + encodeURIComponent(name), {
    method: "PUT",
    body: JSON.stringify({
      input_per_m: numOrZero($("mf-pin").value),
      output_per_m: numOrZero($("mf-pout").value),
      cached_input_per_m: numOrZero($("mf-pcr").value),
      cache_write_per_m: numOrZero($("mf-pcw").value),
    }),
  });
  $("model-form").classList.add("hidden");
  loadModels();
});

async function loadUsage() {
  const evs = await api("/admin/usage/recent?limit=100");
  let html = "<table><thead><tr><th>Time</th><th>Model</th><th>Credential</th><th>Status</th><th>In</th><th>Out</th><th>Cache</th><th>Cost</th><th>ms</th><th>Error</th></tr></thead><tbody>";
  for (const ev of evs) {
    html += `<tr>
      <td>${new Date(ev.ts).toLocaleTimeString()}</td>
      <td><code>${esc(ev.model)}</code></td>
      <td>${ev.credential_id ? esc(ev.credential_id.slice(-8)) : "—"}</td>
      <td>${ev.status_code}</td>
      <td>${fmtN(ev.prompt_tokens)}</td><td>${fmtN(ev.completion_tokens)}</td>
      <td>${ev.cache_hit ? "HIT" : "—"}</td>
      <td>${fmtMoney(ev.cost_usd)}</td>
      <td>${ev.duration_ms}</td>
      <td class="muted">${esc(ev.error || "")}</td></tr>`;
  }
  if (!evs.length) html += '<tr><td colspan="10" class="muted">No requests logged yet</td></tr>';
  $("usage-table").innerHTML = html + "</tbody></table>";
}
$("usage-refresh").addEventListener("click", loadUsage);

async function loadCache() {
  const st = await api("/admin/cache/stats");
  $("cache-cards").innerHTML =
    statCard("Entries", fmtN(st.entries)) +
    statCard("Memory", (st.bytes / 1048576).toFixed(1) + " MB") +
    statCard("Hits", fmtN(st.hits)) +
    statCard("Misses", fmtN(st.misses)) +
    statCard("Hit ratio", (st.hit_ratio * 100).toFixed(1) + "%") +
    statCard("Evictions", fmtN(st.evictions));
}
$("cache-flush").addEventListener("click", async () => { await api("/admin/cache/flush", { method: "POST" }); loadCache(); });

if (MASTER_KEY) {
  api("/admin/auth/verify", { method: "POST", body: JSON.stringify({ key: MASTER_KEY }) })
    .then(() => { show("app"); loadAll(); })
    .catch(() => show("login"));
} else {
  show("login");
}
