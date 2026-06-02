/**
 * Project management and fact board
 */
let projectsCache = [];
let projectsCacheAll = [];
let currentProjectId = null;
let currentProjectTab = 'facts';
const projectNameById = {};
let _projectsListReady = false;
let _projectsFetchPromise = null;

const PROJECT_ACTIVE_KEY = 'cyberstrike.activeProjectId';

function tp(key, opts) {
    if (typeof window.t === 'function') return window.t(key, opts);
    return key;
}

function tpFmt(key, fallback, opts) {
    const text = tp(key, opts);
    if (!text || text === key) return fallback;
    return text;
}

/** Aligned with backend internal/project/fact_template.go */
const FACT_ATTACK_CHAIN_BODY_TEMPLATE = `## Conclusion (verifiable, one sentence)
<Do not only write "vulnerability exists"; specify type + location + trigger condition>

## Target and entry point
- Target: <URL / IP:Port / hostname>
- Entry: <path / endpoint / parameter>
- Preconditions: <anonymous / role / Cookie / other dependencies>

## Attack chain (step-by-step reproducible)
1. <Recon/discovery>
2. <Exploit/trigger>
3. <Impact proof (file read, RCE echo, unauthorized data, etc.)>

## Exploit / POC
### Request
\`\`\`http
<METHOD> <path> HTTP/1.1
Host: ...
...

<body>
\`\`\`

### Response / observation
<Key response fragment, status code, differences>

### Command / script (if any)
\`\`\`bash
<command>
\`\`\`

## Key evidence
- <Tool output summary / screenshot path / conversation or message ID>

## Links
- related_vulnerability_id: <optional>
- Dependent facts: <fact_key, such as auth/session_cookie>

## Notes and uncertainty
<Hypotheses to verify, environment differences, bypass attempts>`;

const FACT_ENV_BODY_TEMPLATE = `## Summary
<Core finding for this fact>

## Details
<Ports/versions/paths/credential traits/business rules/etc.>

## Source and evidence
<Command output, response fragments, discovery time>

## Links
- related fact_key: <optional>`;

const FACT_ATTACK_CHAIN_PREFIXES = ['finding/', 'chain/', 'exploit/', 'poc/'];
const FACT_ATTACK_CHAIN_CATEGORIES = new Set(['finding', 'chain', 'exploit', 'poc', 'vuln']);

function requiresAttackChainFact(category, factKey) {
    const c = (category || '').trim().toLowerCase();
    if (FACT_ATTACK_CHAIN_CATEGORIES.has(c)) return true;
    const key = (factKey || '').trim().toLowerCase();
    return FACT_ATTACK_CHAIN_PREFIXES.some((p) => key.startsWith(p));
}

function isSparseFactBody(category, factKey, body) {
    if (!requiresAttackChainFact(category, factKey)) return false;
    const text = (body || '').trim();
    if (!text) return true;
    const lower = text.toLowerCase();
    const hasSteps =
        lower.includes('Attack chain') ||
        lower.includes('## Attack') ||
        lower.includes('## exploit') ||
        lower.includes('## poc');
    const hasHTTP =
        lower.includes('```http') ||
        lower.includes('```bash') ||
        lower.includes('curl ') ||
        lower.includes('get ') ||
        lower.includes('post ');
    const hasReq = lower.includes('Request') || lower.includes('Response') || lower.includes('payload');
    return !(hasSteps || hasHTTP || hasReq);
}

function formatFactBodyBadge(f) {
    if (!requiresAttackChainFact(f.category, f.fact_key)) {
        const hasBody = !!(f.body || '').trim();
        return `<span class="projects-fact-badge projects-fact-badge--na" title="${escapeHtml(tp('projects.factBodyEnvTitle'))}">${hasBody ? escapeHtml(tp('projects.factBodyHasDetail')) : '—'}</span>`;
    }
    if (isSparseFactBody(f.category, f.fact_key, f.body)) {
        return `<span class="projects-fact-badge projects-fact-badge--warn" title="${escapeHtml(tp('projects.factBodySparseTitle'))}">${escapeHtml(tp('projects.factBodySparse'))}</span>`;
    }
    return `<span class="projects-fact-badge projects-fact-badge--ok" title="${escapeHtml(tp('projects.factBodyReproducibleTitle'))}">${escapeHtml(tp('projects.factBodyReproducible'))}</span>`;
}

function updateFactFormHints() {
    const cat = document.getElementById('fact-modal-category')?.value || '';
    const key = document.getElementById('fact-modal-key')?.value || '';
    const body = document.getElementById('fact-modal-body')?.value || '';
    const hint = document.getElementById('fact-modal-body-hint');
    if (!hint) return;
    if (requiresAttackChainFact(cat, key)) {
        const sparse = isSparseFactBody(cat, key, body);
        hint.textContent = sparse
            ? tp('projects.factHintAttackSparse')
            : tp('projects.factHintAttackReady');
        hint.classList.toggle('projects-field-hint--warn', sparse);
    } else {
        hint.textContent = tp('projects.factHintEnv');
        hint.classList.remove('projects-field-hint--warn');
    }
}

function insertFactBodyTemplate(kind) {
    const ta = document.getElementById('fact-modal-body');
    if (!ta) return;
    const tpl = kind === 'env' ? FACT_ENV_BODY_TEMPLATE : FACT_ATTACK_CHAIN_BODY_TEMPLATE;
    if (ta.value.trim() && !confirm(tp('projects.confirmOverwriteBodyTemplate'))) return;
    ta.value = tpl;
    updateFactFormHints();
    ta.focus();
}

function getActiveProjectId() {
    try {
        return localStorage.getItem(PROJECT_ACTIVE_KEY) || '';
    } catch (e) {
        return '';
    }
}

function setActiveProjectId(id) {
    try {
        if (id) localStorage.setItem(PROJECT_ACTIVE_KEY, id);
        else localStorage.removeItem(PROJECT_ACTIVE_KEY);
    } catch (e) { /* ignore */ }
}

function rebuildProjectNameMap(list) {
    Object.keys(projectNameById).forEach((k) => delete projectNameById[k]);
    (list || []).forEach((p) => {
        if (p && p.id) projectNameById[p.id] = p.name || p.id;
    });
}

async function fetchProjectsList(includeArchived) {
    const showArchived = includeArchived || document.getElementById('projects-show-archived')?.checked;
    const url = showArchived ? '/api/projects?limit=200' : '/api/projects?status=active&limit=200';
    const res = await apiFetch(url);
    if (!res.ok) throw new Error(tp('projects.loadProjectsFailed'));
    const data = await res.json();
    projectsCache = Array.isArray(data) ? data : [];
    rebuildProjectNameMap(projectsCache);
    _projectsListReady = true;
    return projectsCache;
}

/** Internal UI state handling. */
async function ensureProjectsLoaded(force) {
    if (!force && _projectsListReady) return projectsCache;
    if (!force && _projectsFetchPromise) return _projectsFetchPromise;
    _projectsFetchPromise = fetchProjectsList(false)
        .catch((e) => {
            _projectsListReady = false;
            throw e;
        })
        .finally(() => {
            _projectsFetchPromise = null;
        });
    return _projectsFetchPromise;
}

function prefetchProjectsForChat() {
    ensureProjectsLoaded().catch(() => {});
}

/** Internal UI state handling. */
async function ensureDefaultActiveProjectForNewChat() {
    try {
        await ensureProjectsLoaded();
        const cur = getActiveProjectId();
        if (cur && isActiveChatProjectId(cur)) return cur;
        const first =
            projectsCache.find((p) => p.pinned && p.status !== 'archived') ||
            projectsCache.find((p) => p.status !== 'archived');
        if (first) {
            setActiveProjectId(first.id);
            return first.id;
        }
    } catch (e) {
        console.warn(e);
    }
    return '';
}

function getProjectName(id) {
    return projectNameById[id] || id || '';
}

function initProjectsModalEscape() {
    if (window._projectsModalEscapeBound) return;
    window._projectsModalEscapeBound = true;
    document.addEventListener('keydown', (e) => {
        if (e.key !== 'Escape') return;
        if (isProjectsOverlayVisible('project-modal')) closeProjectModal();
        else if (isProjectsOverlayVisible('fact-modal')) closeFactModal();
        else if (isProjectsOverlayVisible('fact-detail-modal')) closeFactDetailModal();
    });
}

async function initProjectsPage() {
    const page = document.getElementById('page-projects');
    if (!page || page.style.display === 'none') return;
    initProjectsModalEscape();
    syncProjectsModalBodyLock();
    updateProjectsDetailVisibility();
    await loadProjectsList();
    if (!currentProjectId && projectsCache.length) {
        const fromHash = new URLSearchParams(window.location.hash.split('?')[1] || '').get('id');
        currentProjectId = fromHash || projectsCache[0].id;
    }
    renderProjectsSidebar();
    if (currentProjectId) {
        await selectProject(currentProjectId);
    }
}

async function loadProjectsList() {
    await fetchProjectsList();
    renderProjectsSidebar();
    if (typeof refreshChatProjectSelector === 'function') {
        refreshChatProjectSelector();
    }
    if (typeof refreshVulnerabilityProjectFilter === 'function') {
        refreshVulnerabilityProjectFilter();
    }
}

function projectInitial(name) {
    const s = (name || 'P').trim();
    return s ? s.charAt(0).toUpperCase() : 'P';
}

function updateProjectsDetailVisibility() {
    const main = document.getElementById('projects-detail-main');
    const placeholder = document.getElementById('projects-detail-placeholder');
    const inner = document.getElementById('projects-detail-inner');
    const show = !!currentProjectId;
    if (main) main.classList.toggle('has-project', show);
    if (placeholder) placeholder.hidden = show;
    if (inner) inner.hidden = !show;
}

function updateProjectsListCount() {
    const el = document.getElementById('projects-list-count');
    if (el) el.textContent = String(projectsCache.length);
}

/** Internal UI state handling. */
const FACT_CATEGORY_BADGE = {
    target: 'projects-category--target',
    auth: 'projects-category--auth',
    infra: 'projects-category--infra',
    business: 'projects-category--business',
    finding: 'projects-category--finding',
    chain: 'projects-category--chain',
    exploit: 'projects-category--exploit',
    poc: 'projects-category--poc',
    note: 'projects-category--note',
    vuln: 'projects-category--exploit',
};

function formatCategoryBadge(category) {
    const raw = (category || '').trim();
    const c = raw.toLowerCase() || 'note';
    const cls = FACT_CATEGORY_BADGE[c] || 'projects-category--custom';
    return `<span class="projects-category ${cls}">${escapeHtml(raw || '—')}</span>`;
}

function formatConfidenceBadge(confidence) {
    const c = (confidence || '').toLowerCase();
    let cls = 'projects-confidence--tentative';
    let label = c || '—';
    if (c === 'confirmed') {
        cls = 'projects-confidence--confirmed';
        label = tp('projects.confidenceConfirmed');
    } else if (c === 'deprecated') {
        cls = 'projects-confidence--deprecated';
        label = tp('projects.confidenceDeprecated');
    } else if (c === 'tentative') {
        label = tp('projects.confidenceTentative');
    }
    return `<span class="projects-confidence ${cls}">${escapeHtml(label)}</span>`;
}

function renderProjectFactActions(keyEsc, idEsc, confidence) {
    const isDeprecated = (confidence || '').toLowerCase() === 'deprecated';
    const toggleBtn = isDeprecated
        ? `<button type="button" class="projects-action-btn projects-action-btn--restore" data-fact-key="${keyEsc}" onclick="restoreProjectFactByKey(this.dataset.factKey)" title="${escapeHtml(tp('projects.restoreTitle'))}">${escapeHtml(tp('projects.restore'))}</button>`
        : `<button type="button" class="projects-action-btn projects-action-btn--mute" data-fact-key="${keyEsc}" onclick="deprecateProjectFactByKey(this.dataset.factKey)" title="${escapeHtml(tp('projects.deprecateTitle'))}">${escapeHtml(tp('projects.deprecate'))}</button>`;
    return `<div class="projects-table-actions">
        <button type="button" class="projects-action-btn projects-action-btn--edit" data-fact-key="${keyEsc}" onclick="showEditFactModal(this.dataset.factKey)" title="${escapeHtml(tp('projects.editTitle'))}">${escapeHtml(tp('common.edit'))}</button>
        <button type="button" class="projects-action-btn projects-action-btn--view" data-fact-key="${keyEsc}" onclick="viewProjectFactBody(this.dataset.factKey)" title="${escapeHtml(tp('projects.viewBodyTitle'))}">${escapeHtml(tp('projects.details'))}</button>
        ${toggleBtn}
        <button type="button" class="projects-action-btn projects-action-btn--danger" data-fact-id="${idEsc}" onclick="deleteProjectFact(this.dataset.factId)" title="${escapeHtml(tp('projects.deleteForeverTitle'))}">${escapeHtml(tp('common.delete'))}</button>
    </div>`;
}

function formatSeverityBadge(severity) {
    const s = (severity || 'info').toLowerCase();
    const cls = 'projects-severity--' + (['critical', 'high', 'medium', 'low', 'info'].includes(s) ? s : 'info');
    return `<span class="projects-severity ${cls}">${escapeHtml(severity || '—')}</span>`;
}

function formatVulnStatusBadge(status) {
    const s = (status || 'open').toLowerCase();
    const labelMap = {
        open: 'vulnerabilityPage.statusOpen',
        confirmed: 'vulnerabilityPage.statusConfirmed',
        fixed: 'vulnerabilityPage.statusFixed',
        false_positive: 'vulnerabilityPage.statusFalsePositive',
    };
    const label = labelMap[s] ? tp(labelMap[s]) : status || '—';
    const cls = ['open', 'confirmed', 'fixed', 'false_positive'].includes(s) ? s : 'open';
    return `<span class="status-badge status-${escapeHtml(cls)}">${escapeHtml(label)}</span>`;
}

let _projectVulnsFilterDebounce = null;

function buildProjectVulnsQueryParams() {
    const params = new URLSearchParams();
    params.set('project_id', currentProjectId);
    params.set('limit', '200');
    const search = document.getElementById('project-vulns-search')?.value?.trim();
    const severity = document.getElementById('project-vulns-filter-severity')?.value?.trim();
    const status = document.getElementById('project-vulns-filter-status')?.value?.trim();
    if (search) params.set('q', search);
    if (severity) params.set('severity', severity);
    if (status) params.set('status', status);
    return params;
}

function projectVulnsHasActiveFilter() {
    return !!(
        document.getElementById('project-vulns-search')?.value?.trim() ||
        document.getElementById('project-vulns-filter-severity')?.value ||
        document.getElementById('project-vulns-filter-status')?.value
    );
}

function debouncedLoadProjectVulnerabilities() {
    if (_projectVulnsFilterDebounce) clearTimeout(_projectVulnsFilterDebounce);
    _projectVulnsFilterDebounce = setTimeout(() => {
        _projectVulnsFilterDebounce = null;
        loadProjectVulnerabilities();
    }, 280);
}

function getProjectsListFilter() {
    return (document.getElementById('projects-list-search')?.value || '').trim().toLowerCase();
}

function filterProjectsList() {
    renderProjectsSidebar();
}

function renderProjectsSidebar() {
    const el = document.getElementById('projects-list');
    if (!el) return;
    updateProjectsListCount();
    const q = getProjectsListFilter();
    const list = q
        ? projectsCache.filter((p) => (p.name || '').toLowerCase().includes(q) || (p.description || '').toLowerCase().includes(q))
        : projectsCache;
    if (!projectsCache.length) {
        el.innerHTML =
            `<div class="projects-empty">${escapeHtml(tp('projects.noProjects'))}<br><button type="button" class="btn-primary btn-small projects-empty-btn" onclick="showNewProjectModal()">${escapeHtml(tp('projects.newProject'))}</button></div>`;
        updateProjectsDetailVisibility();
        return;
    }
    if (!list.length) {
        el.innerHTML = `<div class="projects-empty">${escapeHtml(tp('projects.noMatchingProjects'))}</div>`;
        updateProjectsDetailVisibility();
        return;
    }
    el.innerHTML = list.map((p) => {
        const active = p.id === currentProjectId ? ' is-active' : '';
        const archived = p.status === 'archived' ? ' is-archived' : '';
        const badges = [
            p.pinned ? `<span class="projects-list-item-badge">${escapeHtml(tp('projects.pinned'))}</span>` : '',
            p.status === 'archived' ? `<span class="projects-list-item-badge">${escapeHtml(tp('projects.archived'))}</span>` : '',
        ].join('');
        return `<div class="projects-list-item${active}${archived}" data-id="${escapeHtml(p.id)}" onclick="selectProject('${escapeHtml(p.id)}')">
            <div class="projects-list-item-body">
                <div class="projects-list-item-name">${escapeHtml(p.name)}${badges}</div>
                <div class="projects-list-item-meta">${formatProjectTime(p.updated_at)}</div>
            </div>
        </div>`;
    }).join('');
    updateProjectsDetailVisibility();
}

function updateProjectStatusPill(status) {
    const el = document.getElementById('projects-detail-status');
    if (!el) return;
    const archived = status === 'archived';
    el.textContent = archived ? tp('projects.statusArchived') : tp('projects.statusActive');
    el.className = 'projects-status-pill ' + (archived ? 'projects-status-pill--archived' : 'projects-status-pill--active');
}

function updateProjectStats(stats) {
    const s = stats || {};
    const f = document.getElementById('project-stat-facts');
    const v = document.getElementById('project-stat-vulns');
    const c = document.getElementById('project-stat-conversations');
    const sparse = document.getElementById('project-stat-sparse');
    const fc = s.fact_count ?? s.factCount ?? 0;
    const vc = s.vuln_count ?? s.vulnCount ?? 0;
    const cc = s.conversation_count ?? s.conversationCount ?? 0;
    const sc = s.sparse_fact_count ?? s.sparseFactCount ?? 0;
    if (f) f.textContent = tpFmt('projects.statsFacts', `${fc} facts`, { count: fc });
    if (v) v.textContent = tpFmt('projects.statsVulns', `${vc} vulnerabilities`, { count: vc });
    if (c) c.textContent = tpFmt('projects.statsConversations', `${cc} conversations`, { count: cc });
    if (sparse) {
        if (sc > 0) {
            sparse.hidden = false;
            sparse.textContent = tpFmt('projects.statsSparse', `${sc} to complete`, { count: sc });
        } else {
            sparse.hidden = true;
        }
    }
}

async function selectProject(id) {
    currentProjectId = id;
    const searchEl = document.getElementById('project-facts-search');
    const catEl = document.getElementById('project-facts-filter-category');
    const confEl = document.getElementById('project-facts-filter-confidence');
    const sparseEl = document.getElementById('project-facts-filter-sparse');
    const vulnSearchEl = document.getElementById('project-vulns-search');
    const vulnSevEl = document.getElementById('project-vulns-filter-severity');
    const vulnStatusEl = document.getElementById('project-vulns-filter-status');
    if (searchEl) searchEl.value = '';
    if (catEl) catEl.value = '';
    if (confEl) confEl.value = '';
    if (sparseEl) sparseEl.checked = false;
    if (vulnSearchEl) vulnSearchEl.value = '';
    if (vulnSevEl) vulnSevEl.value = '';
    if (vulnStatusEl) vulnStatusEl.value = '';
    renderProjectsSidebar();
    updateProjectsDetailVisibility();
    try {
        const res = await apiFetch(`/api/projects/${id}`);
        if (!res.ok) throw new Error(tp('projects.projectNotFound'));
        const p = await res.json();
        const titleEl = document.getElementById('projects-detail-title');
        if (titleEl) titleEl.textContent = p.name || tp('projects.defaultProjectName');
        document.getElementById('project-edit-name').value = p.name || '';
        document.getElementById('project-edit-description').value = p.description || '';
        document.getElementById('project-edit-scope').value = p.scope_json || '';
        const statusEl = document.getElementById('project-edit-status');
        if (statusEl) statusEl.value = p.status || 'active';
        const pinEl = document.getElementById('project-edit-pinned');
        if (pinEl) pinEl.checked = !!p.pinned;
        updateProjectStatusPill(p.status || 'active');
        const metaEl = document.getElementById('projects-detail-meta');
        if (metaEl) metaEl.textContent = tpFmt('projects.updatedPrefix', `Updated ${formatProjectTime(p.updated_at)}`, { time: formatProjectTime(p.updated_at) });
        const descEl = document.getElementById('projects-detail-desc');
        if (descEl) {
            const desc = (p.description || '').trim();
            if (desc) {
                descEl.textContent = desc;
                descEl.hidden = false;
            } else {
                descEl.textContent = '';
                descEl.hidden = true;
            }
        }
        projectNameById[p.id] = p.name || p.id;
    } catch (e) {
        console.warn(e);
    }
    await refreshProjectHeaderStats();
    switchProjectTab(currentProjectTab);
}

function switchProjectTab(tab) {
    currentProjectTab = tab;
    ['facts', 'conversations', 'vulns', 'settings'].forEach((t) => {
        const btn = document.getElementById(`project-tab-${t}`);
        const panel = document.getElementById(`project-panel-${t}`);
        if (btn) btn.classList.toggle('is-active', t === tab);
        if (panel) panel.hidden = t !== tab;
    });
    if (tab === 'facts') loadProjectFacts();
    if (tab === 'conversations') loadProjectConversations();
    if (tab === 'vulns') loadProjectVulnerabilities();
}

function buildProjectFactsQueryParams() {
    const params = new URLSearchParams();
    params.set('limit', '200');
    const search = document.getElementById('project-facts-search')?.value?.trim();
    const category = document.getElementById('project-facts-filter-category')?.value?.trim();
    const confidence = document.getElementById('project-facts-filter-confidence')?.value?.trim();
    const sparseOnly = document.getElementById('project-facts-filter-sparse')?.checked;
    const hideDeprecated = document.getElementById('project-facts-filter-hide-deprecated')?.checked;
    if (search) params.set('search', search);
    if (category) params.set('category', category);
    if (confidence) params.set('confidence', confidence);
    if (sparseOnly) params.set('sparse_only', 'true');
    if (hideDeprecated) params.set('exclude_deprecated', 'true');
    return params;
}

function debouncedLoadProjectFacts() {
    if (_projectFactsFilterDebounce) clearTimeout(_projectFactsFilterDebounce);
    _projectFactsFilterDebounce = setTimeout(() => {
        _projectFactsFilterDebounce = null;
        loadProjectFacts();
    }, 280);
}

async function loadProjectFacts() {
    const tbody = document.getElementById('project-facts-tbody');
    if (!tbody || !currentProjectId) return;
    tbody.innerHTML = `<tr class="is-empty-row"><td colspan="7">${escapeHtml(tp('common.loading'))}</td></tr>`;
    const qs = buildProjectFactsQueryParams().toString();
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts?${qs}`);
    if (!res.ok) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="7">${escapeHtml(tp('common.loadFailed'))}</td></tr>`;
        return;
    }
    const facts = await res.json();
    if (!facts.length) {
        const hasFilter =
            document.getElementById('project-facts-search')?.value?.trim() ||
            document.getElementById('project-facts-filter-category')?.value ||
            document.getElementById('project-facts-filter-confidence')?.value ||
            document.getElementById('project-facts-filter-sparse')?.checked;
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="7">${
            hasFilter ? tp('projects.noMatchingFacts') : tp('projects.noFacts')
        }</td></tr>`;
        refreshProjectHeaderStats();
        return;
    }
    tbody.innerHTML = facts.map((f) => {
        const keyEsc = escapeHtml(f.fact_key);
        const idEsc = escapeHtml(f.id);
        const vulnLink = f.related_vulnerability_id
            ? `<span class="projects-fact-vuln-link" title="${escapeHtml(tp('projects.relatedVulnIdTitle'))}">${escapeHtml(f.related_vulnerability_id.slice(0, 8))}…</span>`
            : '';
        return `<tr>
            <td><code>${keyEsc}</code>${vulnLink}</td>
            <td>${formatCategoryBadge(f.category)}</td>
            <td class="cell-summary" title="${escapeHtml(f.summary)}">${escapeHtml(f.summary)}</td>
            <td>${formatFactBodyBadge(f)}</td>
            <td>${formatConfidenceBadge(f.confidence)}</td>
            <td>${formatProjectTime(f.updated_at, f.created_at)}</td>
            <td class="col-actions">${renderProjectFactActions(keyEsc, idEsc, f.confidence)}</td>
        </tr>`;
    }).join('');
    refreshProjectHeaderStats();
}

async function refreshProjectHeaderStats() {
    if (!currentProjectId) return;
    try {
        const res = await apiFetch(`/api/projects/${currentProjectId}/stats`);
        if (!res.ok) return;
        const stats = await res.json();
        updateProjectStats(stats);
    } catch (e) {
        console.warn(e);
    }
}

async function loadProjectConversations() {
    const tbody = document.getElementById('project-conversations-tbody');
    if (!tbody || !currentProjectId) return;
    tbody.innerHTML = `<tr class="is-empty-row"><td colspan="3">${escapeHtml(tp('common.loading'))}</td></tr>`;
    const res = await apiFetch(`/api/projects/${currentProjectId}/conversations?limit=100`);
    if (!res.ok) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="3">${escapeHtml(tp('common.loadFailed'))}</td></tr>`;
        return;
    }
    const data = await res.json();
    const items = data.conversations || [];
    if (!items.length) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="3">${escapeHtml(tp('projects.noBoundConversations'))}</td></tr>`;
        return;
    }
    tbody.innerHTML = items
        .map((conv) => {
            const id = conv.id;
            const idEsc = escapeHtml(id);
            const title = escapeHtml(conv.title || tp('projects.untitledConversation'));
            const updated = formatProjectTime(conv.updatedAt || conv.updated_at, conv.createdAt || conv.created_at);
            return `<tr>
            <td class="cell-summary" title="${title}">${title}</td>
            <td>${escapeHtml(updated)}</td>
            <td class="col-actions">
                <div class="projects-table-actions">
                    <button type="button" class="projects-action-btn projects-action-btn--view" data-conv-id="${idEsc}" onclick="openProjectConversation(this.dataset.convId)">${escapeHtml(tp('projects.open'))}</button>
                    <button type="button" class="projects-action-btn projects-action-btn--mute" data-conv-id="${idEsc}" onclick="unbindConversationFromProject(this.dataset.convId)" title="${escapeHtml(tp('projects.unbindProjectTitle'))}">${escapeHtml(tp('projects.unbind'))}</button>
                </div>
            </td>
        </tr>`;
        })
        .join('');
}

function openProjectConversation(conversationId) {
    if (!conversationId) return;
    if (typeof switchPage === 'function') {
        switchPage('chat');
    }
    setTimeout(() => {
        if (typeof loadConversation === 'function') {
            loadConversation(conversationId);
        }
    }, 200);
}

async function unbindConversationFromProject(conversationId) {
    if (!conversationId || !confirm(tp('projects.confirmUnbindConversation'))) return;
    const res = await apiFetch(`/api/conversations/${encodeURIComponent(conversationId)}/project`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ projectId: '' }),
    });
    if (!res.ok) return alert(tp('projects.unbindFailed'));
    loadProjectConversations();
    refreshProjectHeaderStats();
}

let _factDetailKey = null;
let _factDetailFact = null;
let _projectFactsFilterDebounce = null;

async function viewProjectFactBody(factKey) {
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts?fact_key=${encodeURIComponent(factKey)}`);
    if (!res.ok) return alert(tp('common.loadFailed'));
    const f = await res.json();
    _factDetailKey = f.fact_key;
    _factDetailFact = f;
    document.getElementById('fact-detail-title').textContent = `[${f.fact_key}]`;
    const metaParts = [
        tpFmt('projects.factMetaCategory', `Category: ${f.category}`, { value: f.category }),
        tpFmt('projects.factMetaConfidence', `Confidence: ${f.confidence}`, { value: f.confidence }),
        tpFmt('projects.factMetaUpdated', `Updated: ${formatProjectTime(f.updated_at, f.created_at)}`, {
            time: formatProjectTime(f.updated_at, f.created_at),
        }),
    ];
    if (f.related_vulnerability_id) metaParts.push(tpFmt('projects.factMetaRelatedVuln', `Related vulnerability: ${f.related_vulnerability_id}`, { value: f.related_vulnerability_id }));
    if (f.source_conversation_id) metaParts.push(tpFmt('projects.factMetaSourceConversation', `Source conversation: ${f.source_conversation_id}`, { value: f.source_conversation_id }));
    if (f.supersedes_fact_id) metaParts.push(tp('projects.factMetaHasPrevious'));
    document.getElementById('fact-detail-meta').textContent = metaParts.join(' · ');
    document.getElementById('fact-detail-body').textContent = f.body || tp('projects.emptyBody');
    const warnEl = document.getElementById('fact-detail-sparse-warn');
    if (warnEl) {
        if (isSparseFactBody(f.category, f.fact_key, f.body)) {
            warnEl.hidden = false;
            warnEl.textContent = tp('projects.factSparseWarn');
        } else {
            warnEl.hidden = true;
            warnEl.textContent = '';
        }
    }
    const prevWrap = document.getElementById('fact-detail-prev-wrap');
    if (prevWrap) {
        prevWrap.hidden = true;
        if (f.id && f.supersedes_fact_id) {
            try {
                const prevRes = await apiFetch(
                    `/api/projects/${currentProjectId}/facts/${encodeURIComponent(f.id)}/previous-version`,
                );
                if (prevRes.ok) {
                    const prev = await prevRes.json();
                    prevWrap.hidden = false;
                    document.getElementById('fact-detail-prev-meta').textContent = tpFmt(
                        'projects.factPreviousMeta',
                        `Archived at ${formatProjectTime(prev.archived_at)} · Summary: ${prev.summary || '—'} · Confidence: ${prev.confidence || '—'}`,
                        {
                            time: formatProjectTime(prev.archived_at),
                            summary: prev.summary || '—',
                            confidence: prev.confidence || '—',
                        },
                    );
                    document.getElementById('fact-detail-prev-body').textContent = prev.body || tp('projects.emptyBody');
                }
            } catch (e) {
                console.warn(e);
            }
        }
    }
    const linkBtn = document.getElementById('fact-detail-link-vuln-btn');
    const createBtn = document.getElementById('fact-detail-create-vuln-btn');
    if (linkBtn) linkBtn.hidden = false;
    if (createBtn) createBtn.hidden = false;
    openProjectsOverlay('fact-detail-modal');
}

function editFactFromDetail() {
    const key = _factDetailKey;
    closeFactDetailModal();
    if (key) showEditFactModal(key);
}

function closeFactDetailModal() {
    closeProjectsOverlay('fact-detail-modal');
    _factDetailKey = null;
    _factDetailFact = null;
}

async function linkFactToExistingVulnerability() {
    const f = _factDetailFact;
    if (!f || !currentProjectId) return;
    const res = await apiFetch(`/api/vulnerabilities?project_id=${encodeURIComponent(currentProjectId)}&limit=50`);
    if (!res.ok) return alert(tp('projects.loadVulnerabilityListFailed'));
    const data = await res.json();
    const items = data.Vulnerabilities || data.vulnerabilities || data.items || [];
    if (!items.length) return alert(tp('projects.noVulnerabilitiesInProject'));
    const lines = items.map((v, i) => `${i + 1}. [${v.severity}] ${v.title} (${v.id})`);
    const pick = prompt(
        tp('projects.promptLinkFactToVuln', {
            factKey: f.fact_key,
            lines: lines.join('\n'),
            interpolation: { escapeValue: false },
        }) || `Enter index to link fact "${f.fact_key}":\n\n${lines.join('\n')}`,
    );
    if (pick == null || pick === '') return;
    const idx = parseInt(pick, 10) - 1;
    if (Number.isNaN(idx) || idx < 0 || idx >= items.length) return alert(tp('projects.invalidIndex'));
    const vulnId = items[idx].id;
    const upd = await apiFetch(`/api/projects/${currentProjectId}/facts/${encodeURIComponent(f.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            fact_key: f.fact_key,
            category: f.category,
            summary: f.summary,
            body: f.body || '',
            confidence: f.confidence,
            related_vulnerability_id: vulnId,
        }),
    });
    if (!upd.ok) return alert(tp('projects.linkFailed'));
    alert(tp('projects.linkSuccess'));
    closeFactDetailModal();
    loadProjectFacts();
}

async function createVulnerabilityFromCurrentFact() {
    const f = _factDetailFact;
    if (!f || !currentProjectId) return;
    let convId =
        (f.source_conversation_id || '').trim() ||
        (typeof window.currentConversationId === 'string' ? window.currentConversationId.trim() : '');
    if (!convId) {
        convId = prompt(tp('projects.promptConversationIdForVulnCreate'), '')?.trim() || '';
    }
    if (!convId) return alert(tp('projects.cancelledNoConversationId'));
    const severity = inferSeverityFromFact(f);
    const body = {
        conversation_id: convId,
        project_id: currentProjectId,
        title: (f.summary || f.fact_key).slice(0, 200),
        description:
            tp('projects.generatedFromFact', {
                factKey: f.fact_key,
                interpolation: { escapeValue: false },
            }) || `Generated from project fact ${f.fact_key}`,
        severity,
        status: 'open',
        type: f.category || 'finding',
        target: '',
        proof: f.body || '',
        impact: '',
        recommendation: '',
    };
    const res = await apiFetch('/api/vulnerabilities', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });
    if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        return alert(err.error || tp('projects.createVulnerabilityFailed'));
    }
    const vuln = await res.json();
    await apiFetch(`/api/projects/${currentProjectId}/facts/${encodeURIComponent(f.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            fact_key: f.fact_key,
            category: f.category,
            summary: f.summary,
            body: f.body || '',
            confidence: f.confidence,
            related_vulnerability_id: vuln.id,
        }),
    });
    const createdVulnLabel = vuln.title || vuln.id;
    const successMsg = tp('projects.createVulnerabilityAndLinkSuccess', {
        value: createdVulnLabel,
        interpolation: { escapeValue: false },
    });
    alert(successMsg || `Created and linked vulnerability: ${createdVulnLabel}`);
    closeFactDetailModal();
    loadProjectFacts();
    if (currentProjectTab === 'vulns') loadProjectVulnerabilities();
}

function inferSeverityFromFact(f) {
    const c = (f.category || '').toLowerCase();
    const key = (f.fact_key || '').toLowerCase();
    if (c === 'exploit' || c === 'poc' || key.includes('rce') || key.includes('sqli')) return 'high';
    if (c === 'finding' || c === 'chain') return 'medium';
    return 'medium';
}

async function deprecateProjectFactByKey(factKey) {
    if (!confirm(
        tp('projects.confirmDeprecateFact', {
            factKey,
            interpolation: { escapeValue: false },
        }) || `Deprecate fact ${factKey}?`,
    )) return;
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts/deprecate`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ fact_key: factKey }),
    });
    if (!res.ok) return alert(tp('projects.operationFailed'));
    loadProjectFacts();
}

async function restoreProjectFactByKey(factKey) {
    if (!confirm(
        tp('projects.confirmRestoreFact', {
            factKey,
            interpolation: { escapeValue: false },
        }) || `Restore fact ${factKey}? It will re-enter the board index with tentative status.`,
    )) return;
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts/restore`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ fact_key: factKey, confidence: 'tentative' }),
    });
    if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        return alert(err.error || tp('projects.operationFailed'));
    }
    loadProjectFacts();
}

function openVulnerabilitiesForProject(projectId) {
    const pid = projectId || currentProjectId;
    if (!pid) return;
    if (typeof switchPage === 'function') {
        switchPage('vulnerabilities');
    }
    if (typeof window.setVulnerabilityProjectFilter === 'function') {
        window.setVulnerabilityProjectFilter(pid);
    } else {
        window.location.hash = `vulnerabilities?project_id=${encodeURIComponent(pid)}`;
    }
}

async function loadProjectVulnerabilities() {
    const tbody = document.getElementById('project-vulns-tbody');
    if (!tbody || !currentProjectId) return;
    tbody.innerHTML = `<tr class="is-empty-row"><td colspan="4">${escapeHtml(tp('common.loading'))}</td></tr>`;
    const qs = buildProjectVulnsQueryParams().toString();
    const res = await apiFetch(`/api/vulnerabilities?${qs}`);
    if (!res.ok) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="4">${escapeHtml(tp('common.loadFailed'))}</td></tr>`;
        return;
    }
    const data = await res.json();
    const items = data.Vulnerabilities || data.vulnerabilities || data.items || (Array.isArray(data) ? data : []);
    if (!items.length) {
        tbody.innerHTML = `<tr class="is-empty-row"><td colspan="4">${
            projectVulnsHasActiveFilter() ? tp('projects.noMatchingVulns') : tp('projects.noVulnerabilityRecords')
        }</td></tr>`;
        refreshProjectHeaderStats();
        return;
    }
    tbody.innerHTML = items.map((v) => {
        const idEsc = escapeHtml(v.id);
        return `<tr>
            <td class="cell-summary" title="${escapeHtml(v.title)}">${escapeHtml(v.title)}</td>
            <td>${formatSeverityBadge(v.severity)}</td>
            <td>${formatVulnStatusBadge(v.status)}</td>
            <td class="col-actions">
                <div class="projects-table-actions">
                    <button type="button" class="projects-action-btn projects-action-btn--view" data-vuln-id="${idEsc}" onclick="openVulnerabilityDetail(this.dataset.vulnId)">${escapeHtml(tp('common.view'))}</button>
                    <button type="button" class="projects-action-btn projects-action-btn--view" data-vuln-id="${idEsc}" onclick="viewFactsForVulnerability(this.dataset.vulnId)" title="${escapeHtml(tp('projects.viewRelatedFactsTitle'))}">${escapeHtml(tp('projects.facts'))}</button>
                </div>
            </td>
        </tr>`;
    }).join('');
    refreshProjectHeaderStats();
}

function openVulnerabilityDetail(vulnId) {
    openVulnerabilitiesForProject(currentProjectId);
    if (typeof window.setVulnerabilityIdFilter === 'function') {
        setTimeout(() => window.setVulnerabilityIdFilter(vulnId), 300);
    }
}

async function viewFactsForVulnerability(vulnId) {
    if (!currentProjectId) return;
    switchProjectTab('facts');
    const searchEl = document.getElementById('project-facts-search');
    const catEl = document.getElementById('project-facts-filter-category');
    const confEl = document.getElementById('project-facts-filter-confidence');
    const sparseEl = document.getElementById('project-facts-filter-sparse');
    const hideDepEl = document.getElementById('project-facts-filter-hide-deprecated');
    if (searchEl) searchEl.value = '';
    if (catEl) catEl.value = '';
    if (confEl) confEl.value = '';
    if (sparseEl) sparseEl.checked = false;
    if (hideDepEl) hideDepEl.checked = true;
    const params = new URLSearchParams({ limit: '50', related_vulnerability_id: vulnId });
    const res = await apiFetch(`/api/projects/${currentProjectId}/facts?${params}`);
    if (!res.ok) return alert(tp('projects.loadRelatedFactsFailed'));
    const facts = await res.json();
    if (!facts.length) {
        alert(tp('projects.noFactsForVulnerability'));
        loadProjectFacts();
        return;
    }
    if (facts.length === 1) {
        viewProjectFactBody(facts[0].fact_key);
        return;
    }
    const pick = prompt(
        tp('projects.promptChooseFactByIndex', {
            count: facts.length,
            lines: facts.map((f, i) => `${i + 1}. ${f.fact_key}`).join('\n'),
            interpolation: { escapeValue: false },
        }) || `This vulnerability is linked to ${facts.length} facts. Enter index to view:\n${facts.map((f, i) => `${i + 1}. ${f.fact_key}`).join('\n')}`,
    );
    if (pick == null || pick === '') {
        loadProjectFacts();
        return;
    }
    const idx = parseInt(pick, 10) - 1;
    if (facts[idx]) viewProjectFactBody(facts[idx].fact_key);
    else loadProjectFacts();
}

function openProjectsOverlay(id) {
    const el = document.getElementById(id);
    if (!el) return;
    el.style.display = 'flex';
    syncProjectsModalBodyLock();
    const focusTarget = el.querySelector('input.form-input, textarea.form-input, select.form-input');
    if (focusTarget) {
        setTimeout(() => focusTarget.focus(), 80);
    }
}

function isProjectsOverlayVisible(id) {
    const el = document.getElementById(id);
    if (!el) return false;
    const style = window.getComputedStyle(el);
    return style.display !== 'none' && style.visibility !== 'hidden';
}

function hasVisibleProjectsOverlay() {
    const overlays = document.querySelectorAll('.projects-modal-overlay');
    return Array.from(overlays).some((el) => {
        const style = window.getComputedStyle(el);
        return style.display !== 'none' && style.visibility !== 'hidden';
    });
}

function syncProjectsModalBodyLock() {
    if (hasVisibleProjectsOverlay()) document.body.classList.add('projects-modal-open');
    else document.body.classList.remove('projects-modal-open');
}

function closeProjectsOverlay(id) {
    const el = document.getElementById(id);
    if (el) el.style.display = 'none';
    syncProjectsModalBodyLock();
}

function showNewProjectModal() {
    document.getElementById('project-modal-title').textContent = tp('projects.modalNewTitle');
    const sub = document.getElementById('project-modal-subtitle');
    if (sub) sub.textContent = tp('projects.modalNewSubtitle');
    const submitBtn = document.getElementById('project-modal-submit-btn');
    if (submitBtn) submitBtn.textContent = tp('projects.createProject');
    document.getElementById('project-modal-name').value = '';
    document.getElementById('project-modal-description').value = '';
    window._projectModalEditId = null;
    openProjectsOverlay('project-modal');
}

/** Internal UI state handling. */
function showNewProjectModalFromChat() {
    closeChatProjectPanel();
    window._projectModalFromChat = true;
    showNewProjectModal();
}

async function saveProjectModal() {
    const name = document.getElementById('project-modal-name').value.trim();
    if (!name) return alert(tp('projects.enterProjectName'));
    const body = {
        name,
        description: document.getElementById('project-modal-description').value.trim(),
    };
    const editId = window._projectModalEditId;
    const res = editId
        ? await apiFetch(`/api/projects/${editId}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
        : await apiFetch('/api/projects', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        alert(err.error || tp('projects.saveFailed'));
        return;
    }
    const fromChat = !!window._projectModalFromChat;
    window._projectModalFromChat = false;
    closeProjectModal();
    const saved = await res.json();
    await loadProjectsList();
    if (saved.id) {
        if (fromChat && !editId) {
            await applyChatProjectSelection(saved.id);
        } else {
            await selectProject(saved.id);
        }
    }
}

function closeProjectModal() {
    window._projectModalFromChat = false;
    closeProjectsOverlay('project-modal');
}

function formatProjectScopeJson() {
    const el = document.getElementById('project-edit-scope');
    if (!el) return;
    const raw = el.value.trim();
    if (!raw) return;
    try {
        el.value = JSON.stringify(JSON.parse(raw), null, 2);
    } catch (e) {
        alert(tp('projects.invalidJson') + ': ' + (e.message || String(e)));
    }
}

function insertProjectScopeExample() {
    const el = document.getElementById('project-edit-scope');
    if (!el) return;
    const example = {
        targets: ['https://example.com'],
        exclude: ['*.cdn.example.com'],
        notes: tp('projects.scopeNoteAuthorizedWebOnly'),
    };
    el.value = JSON.stringify(example, null, 2);
    el.focus();
}

async function saveProjectSettings() {
    if (!currentProjectId) return;
    const scopeRaw = document.getElementById('project-edit-scope').value.trim();
    if (scopeRaw) {
        try {
            JSON.parse(scopeRaw);
        } catch (e) {
            alert(tp('projects.invalidScopeJson') + ': ' + (e.message || String(e)));
            return;
        }
    }
    const body = {
        name: document.getElementById('project-edit-name').value.trim(),
        description: document.getElementById('project-edit-description').value.trim(),
        scope_json: scopeRaw,
        status: document.getElementById('project-edit-status')?.value || 'active',
        pinned: !!document.getElementById('project-edit-pinned')?.checked,
    };
    const res = await apiFetch(`/api/projects/${currentProjectId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });
    if (!res.ok) return alert(tp('projects.saveFailed'));
    await loadProjectsList();
    await selectProject(currentProjectId);
    alert(tp('projects.saved'));
}

async function archiveCurrentProject() {
    if (!currentProjectId) return;
    const statusEl = document.getElementById('project-edit-status');
    const cur = statusEl?.value || 'active';
    const next = cur === 'archived' ? 'active' : 'archived';
    if (!confirm(next === 'archived' ? tp('projects.confirmArchiveProject') : tp('projects.confirmRestoreProjectActive'))) return;
    const res = await apiFetch(`/api/projects/${currentProjectId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: next }),
    });
    if (!res.ok) return alert(tp('projects.operationFailed'));
    await loadProjectsList();
    await selectProject(currentProjectId);
}

async function deleteCurrentProject() {
    if (!currentProjectId || !confirm(tp('projects.confirmDeleteProject'))) return;
    const deletedId = currentProjectId;
    const deletedIndex = projectsCache.findIndex((p) => p.id === deletedId);
    const res = await apiFetch(`/api/projects/${deletedId}`, { method: 'DELETE' });
    if (!res.ok) return alert(tp('projects.deleteFailed'));
    if (getActiveProjectId() === deletedId) setActiveProjectId('');
    currentProjectId = null;
    await loadProjectsList();
    if (projectsCache.length) {
        const nextIndex = Math.min(deletedIndex >= 0 ? deletedIndex : 0, projectsCache.length - 1);
        await selectProject(projectsCache[nextIndex].id);
    } else {
        updateProjectsDetailVisibility();
    }
}

function resetFactModalForm() {
    window._factModalEditId = null;
    const keyEl = document.getElementById('fact-modal-key');
    if (keyEl) keyEl.disabled = false;
    document.getElementById('fact-modal-title').textContent = tp('projects.addFact');
    document.getElementById('fact-modal-submit-btn').textContent = tp('projects.saveFact');
    document.getElementById('fact-modal-key').value = '';
    document.getElementById('fact-modal-category').value = 'note';
    document.getElementById('fact-modal-summary').value = '';
    document.getElementById('fact-modal-body').value = '';
    document.getElementById('fact-modal-confidence').value = 'tentative';
    const rel = document.getElementById('fact-modal-related-vuln');
    if (rel) rel.value = '';
    updateFactFormHints();
}

function fillFactModalForm(f) {
    window._factModalEditId = f.id;
    document.getElementById('fact-modal-title').textContent = tp('projects.editFact');
    document.getElementById('fact-modal-submit-btn').textContent = tp('projects.saveChanges');
    document.getElementById('fact-modal-key').value = f.fact_key || '';
    const catEl = document.getElementById('fact-modal-category');
    const cat = (f.category || 'note').trim().toLowerCase();
    if (catEl) {
        const known = Array.from(catEl.options).some((o) => o.value === cat);
        if (known) catEl.value = cat;
        else {
            const opt = document.createElement('option');
            opt.value = f.category;
            opt.textContent = tpFmt('projects.customCategoryOption', `${f.category} (custom)`, { value: f.category });
            catEl.appendChild(opt);
            catEl.value = f.category;
        }
    }
    document.getElementById('fact-modal-summary').value = f.summary || '';
    document.getElementById('fact-modal-body').value = f.body || '';
    const conf = (f.confidence || 'tentative').toLowerCase();
    const confEl = document.getElementById('fact-modal-confidence');
    if (confEl) {
        const allowed = ['tentative', 'confirmed', 'deprecated'];
        confEl.value = allowed.includes(conf) ? conf : 'tentative';
    }
    const rel = document.getElementById('fact-modal-related-vuln');
    if (rel) rel.value = f.related_vulnerability_id || '';
    updateFactFormHints();
}

function showAddFactModal() {
    if (!currentProjectId) return alert(tp('projects.selectProjectFirst'));
    resetFactModalForm();
    openProjectsOverlay('fact-modal');
}

async function showEditFactModal(factKey) {
    if (!currentProjectId) return alert(tp('projects.selectProjectFirst'));
    const res = await apiFetch(
        `/api/projects/${currentProjectId}/facts?fact_key=${encodeURIComponent(factKey)}`,
    );
    if (!res.ok) return alert(tp('projects.loadFactFailed'));
    const f = await res.json();
    resetFactModalForm();
    fillFactModalForm(f);
    openProjectsOverlay('fact-modal');
}

function closeFactModal() {
    closeProjectsOverlay('fact-modal');
    resetFactModalForm();
}

async function saveFactModal() {
    const fact_key = document.getElementById('fact-modal-key').value.trim();
    const summary = document.getElementById('fact-modal-summary').value.trim();
    const category = document.getElementById('fact-modal-category').value.trim() || 'note';
    const body = document.getElementById('fact-modal-body').value;
    if (!fact_key || !summary) return alert(tp('projects.factKeySummaryRequired'));
    if (isSparseFactBody(category, fact_key, body)) {
        const ok = confirm(
            tp('projects.confirmSaveSparseFact'),
        );
        if (!ok) return;
    }
    const payload = {
        fact_key,
        category,
        summary,
        body,
        confidence: document.getElementById('fact-modal-confidence').value,
        related_vulnerability_id: document.getElementById('fact-modal-related-vuln')?.value?.trim() || '',
    };
    const editId = window._factModalEditId;
    const res = editId
        ? await apiFetch(`/api/projects/${currentProjectId}/facts/${editId}`, {
              method: 'PUT',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(payload),
          })
        : await apiFetch(`/api/projects/${currentProjectId}/facts`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(payload),
          });
    if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        return alert(err.error || tp('projects.saveFailed'));
    }
    closeFactModal();
    loadProjectFacts();
}

async function deleteProjectFact(id) {
    if (!confirm(tp('projects.confirmDeleteFact'))) return;
    await apiFetch(`/api/projects/${currentProjectId}/facts/${id}`, { method: 'DELETE' });
    loadProjectFacts();
}

function parseProjectDate(t) {
    if (t == null || t === '') return null;
    if (typeof t === 'number' && Number.isFinite(t)) {
        const d = new Date(t);
        return isNaN(d.getTime()) || d.getFullYear() < 2000 ? null : d;
    }
    let s = String(t).trim();
    if (!s || s.startsWith('0001-01-01')) return null;
    let d = new Date(s);
    if (!isNaN(d.getTime()) && d.getFullYear() >= 2000) return d;
    const m = s.match(
        /^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(?:([Zz]|([+-])(\d{2}):?(\d{2}))?)?$/,
    );
    if (m) {
        const ms = m[7] ? parseInt(String(m[7]).slice(0, 3).padEnd(3, '0'), 10) : 0;
        let offMin = 0;
        if (m[8] && m[9] && m[10]) {
            offMin = parseInt(m[10], 10) * 60 + parseInt(m[11] || '0', 10);
            if (m[9] === '-') offMin = -offMin;
        }
        d = new Date(
            Date.UTC(
                parseInt(m[1], 10),
                parseInt(m[2], 10) - 1,
                parseInt(m[3], 10),
                parseInt(m[4], 10),
                parseInt(m[5], 10),
                parseInt(m[6], 10),
                ms,
            ) - offMin * 60 * 1000,
        );
        if (!isNaN(d.getTime()) && d.getFullYear() >= 2000) return d;
    }
    return null;
}

function formatProjectTime(t, fallback) {
    const d = parseProjectDate(t) || (fallback != null ? parseProjectDate(fallback) : null);
    if (!d) return tp('projects.notUpdatedYet');
    const now = Date.now();
    const diff = now - d.getTime();
    if (diff < 60000) return tp('common.justNow');
    if (diff < 3600000) return tp('common.minutesAgo', { n: Math.floor(diff / 60000) });
    if (diff < 86400000) return tp('common.hoursAgo', { n: Math.floor(diff / 3600000) });
    if (diff < 604800000) return tp('common.daysAgo', { n: Math.floor(diff / 86400000) });
    return d.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function escapeHtml(s) {
    if (s == null) return '';
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

function getChatProjectSelection() {
    const convId = window.currentConversationId;
    if (convId) {
        return window._loadedConversationProjectId || '';
    }
    return getActiveProjectId();
}

function isActiveChatProjectId(id) {
    if (!id) return false;
    return projectsCache.some((p) => p.id === id && p.status !== 'archived');
}

/** Internal UI state handling. */
function resolveChatProjectSelection() {
    const raw = getChatProjectSelection();
    if (!raw) return '';
    if (!_projectsListReady) return raw;
    return isActiveChatProjectId(raw) ? raw : '';
}

let _normalizingStaleProject = false;

/** Internal UI state handling. */
async function normalizeStaleChatProjectSelection() {
    if (!_projectsListReady || _normalizingStaleProject) return;
    const raw = getChatProjectSelection();
    if (!raw || isActiveChatProjectId(raw)) return;

    _normalizingStaleProject = true;
    try {
        if (window.currentConversationId) {
            window._loadedConversationProjectId = '';
            try {
                const res = await apiFetch(
                    `/api/conversations/${encodeURIComponent(window.currentConversationId)}/project`,
                    {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ projectId: '' }),
                    }
                );
                if (!res.ok) console.warn(tp('projects.clearStaleProjectBindingFailed'));
            } catch (e) {
                console.warn(e);
            }
        } else {
            setActiveProjectId('');
        }
    } finally {
        _normalizingStaleProject = false;
    }
}

function updateChatProjectButtonLabel() {
    const textEl = document.getElementById('chat-project-text');
    if (!textEl) return;
    const id = resolveChatProjectSelection();
    textEl.textContent = id && projectNameById[id] ? projectNameById[id] : tp('projects.noProject');
}

function renderChatProjectPanelList() {
    const list = document.getElementById('chat-project-list');
    if (!list) return;
    const selected = resolveChatProjectSelection();
    const activeProjects = projectsCache.filter((p) => p.status !== 'archived');
    const items = [{ id: '', name: tp('projects.noProject'), description: tp('projects.noProjectDescription') }, ...activeProjects];
    if (!items.length) {
        list.innerHTML = `<div class="chat-project-panel-empty">${escapeHtml(tp('projects.noProjectsClickCreate'))}</div>`;
        return;
    }
    list.innerHTML = '';
    items.forEach((p) => {
        const isNone = !p.id;
        const isSelected = isNone ? !selected : selected === p.id;
        const desc = isNone
            ? (p.description || '')
            : (p.description || '').trim().slice(0, 80) || tp('projects.sharedFactBoard');
        const projectId = p.id || '';
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'role-selection-item-main' + (isSelected ? ' selected' : '');
        btn.setAttribute('role', 'option');
        btn.onclick = () => {
            selectChatProject(projectId);
        };
        btn.innerHTML = `
                <div class="role-selection-item-icon-main">${isNone ? '—' : '📁'}</div>
                <div class="role-selection-item-content-main">
                    <div class="role-selection-item-name-main">${escapeHtml(p.name || tp('common.untitled'))}</div>
                    <div class="role-selection-item-description-main">${escapeHtml(desc)}</div>
                </div>
                ${isSelected ? '<div class="role-selection-checkmark-main">✓</div>' : ''}
            `;
        list.appendChild(btn);
    });
}

async function renderChatProjectPanel() {
    const list = document.getElementById('chat-project-list');
    if (!list) return;
    list.innerHTML = `<div class="chat-project-panel-loading">${escapeHtml(tp('common.loading'))}</div>`;
    try {
        await ensureProjectsLoaded();
    } catch (e) {
        console.warn(e);
        list.innerHTML = `<div class="chat-project-panel-empty">${escapeHtml(tp('projects.loadFailedRetry'))}</div>`;
        return;
    }
    renderChatProjectPanelList();
}

function closeChatProjectPanel() {
    const panel = document.getElementById('chat-project-panel');
    const btn = document.getElementById('chat-project-btn');
    if (panel) panel.style.display = 'none';
    if (btn) {
        btn.classList.remove('active');
        btn.setAttribute('aria-expanded', 'false');
    }
}

async function toggleChatProjectPanel() {
    const panel = document.getElementById('chat-project-panel');
    const btn = document.getElementById('chat-project-btn');
    if (!panel) return;
    const isHidden = panel.style.display === 'none' || !panel.style.display;
    if (!isHidden) {
        closeChatProjectPanel();
        return;
    }
    if (typeof closeRoleSelectionPanel === 'function') closeRoleSelectionPanel();
    if (typeof closeAgentModePanel === 'function') closeAgentModePanel();
    if (typeof closeChatReasoningPanel === 'function') closeChatReasoningPanel();
    panel.style.display = 'flex';
    if (btn) {
        btn.classList.add('active');
        btn.setAttribute('aria-expanded', 'true');
    }
    await renderChatProjectPanel();
}

async function selectChatProject(projectId) {
    closeChatProjectPanel();
    await applyChatProjectSelection(projectId || '');
}

async function applyChatProjectSelection(projectId) {
    const prev = getChatProjectSelection();
    if (projectId === prev) {
        updateChatProjectButtonLabel();
        return;
    }
    if (window.currentConversationId) {
        try {
            const res = await apiFetch(`/api/conversations/${encodeURIComponent(window.currentConversationId)}/project`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ projectId }),
            });
            if (!res.ok) {
                const err = await res.json().catch(() => ({}));
                throw new Error(err.error || res.statusText);
            }
            window._loadedConversationProjectId = projectId;
            if (typeof showNotification === 'function') {
                showNotification(projectId ? tp('projects.projectBound') : tp('projects.projectUnbound'), 'success');
            }
        } catch (e) {
            console.error(e);
            alert(tp('projects.updateProjectBindingFailed') + ': ' + (e.message || e));
            updateChatProjectButtonLabel();
            return;
        }
    } else {
        setActiveProjectId(projectId);
    }
    updateChatProjectButtonLabel();
}

/** Internal UI state handling. */
async function refreshChatProjectSelector() {
    if (!document.getElementById('chat-project-btn')) return;
    try {
        await ensureProjectsLoaded();
        await normalizeStaleChatProjectSelection();
    } catch (e) {
        console.warn(e);
    }
    updateChatProjectButtonLabel();
    const panel = document.getElementById('chat-project-panel');
    if (panel && panel.style.display === 'flex') {
        renderChatProjectPanelList();
    }
}

async function onChatProjectChange() {
    /* Internal UI state handling. */
    await applyChatProjectSelection(getChatProjectSelection());
}

function initChatProjectSelector() {
    if (window._chatProjectSelectorInited) return;
    window._chatProjectSelectorInited = true;
    if (!window._projectsLanguageListenerBound) {
        window._projectsLanguageListenerBound = true;
        document.addEventListener('languagechange', () => {
            renderProjectsSidebar();
            updateChatProjectButtonLabel();
            const panel = document.getElementById('chat-project-panel');
            if (panel && panel.style.display === 'flex') renderChatProjectPanelList();
            if (currentProjectId) {
                refreshProjectHeaderStats().catch(() => {});
                switchProjectTab(currentProjectTab || 'facts');
            }
        });
    }
    refreshChatProjectSelector().catch(() => {});
    document.addEventListener('click', (e) => {
        const panel = document.getElementById('chat-project-panel');
        const wrapper = document.querySelector('.project-selector-wrapper');
        if (!panel || panel.style.display === 'none' || !panel.style.display) return;
        if (!wrapper?.contains(e.target)) {
            closeChatProjectPanel();
        }
    });
}

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initChatProjectSelector);
} else {
    initChatProjectSelector();
}

window.initProjectsPage = initProjectsPage;
window.showNewProjectModal = showNewProjectModal;
window.showNewProjectModalFromChat = showNewProjectModalFromChat;
window.saveProjectModal = saveProjectModal;
window.closeProjectModal = closeProjectModal;
window.selectProject = selectProject;
window.switchProjectTab = switchProjectTab;
window.showAddFactModal = showAddFactModal;
window.showEditFactModal = showEditFactModal;
window.editFactFromDetail = editFactFromDetail;
window.saveFactModal = saveFactModal;
window.closeFactModal = closeFactModal;
window.closeFactDetailModal = closeFactDetailModal;
window.saveProjectSettings = saveProjectSettings;
window.archiveCurrentProject = archiveCurrentProject;
window.deleteCurrentProject = deleteCurrentProject;
window.refreshChatProjectSelector = refreshChatProjectSelector;
window.onChatProjectChange = onChatProjectChange;
window.toggleChatProjectPanel = toggleChatProjectPanel;
window.closeChatProjectPanel = closeChatProjectPanel;
window.selectChatProject = selectChatProject;
window.prefetchProjectsForChat = prefetchProjectsForChat;
window.ensureDefaultActiveProjectForNewChat = ensureDefaultActiveProjectForNewChat;
window.getActiveProjectId = getActiveProjectId;
window.getProjectName = getProjectName;
window.viewProjectFactBody = viewProjectFactBody;
window.insertFactBodyTemplate = insertFactBodyTemplate;
window.updateFactFormHints = updateFactFormHints;
window.deprecateProjectFactByKey = deprecateProjectFactByKey;
window.restoreProjectFactByKey = restoreProjectFactByKey;
window.openVulnerabilitiesForProject = openVulnerabilitiesForProject;
window.openVulnerabilityDetail = openVulnerabilityDetail;
window.filterProjectsList = filterProjectsList;
window.debouncedLoadProjectFacts = debouncedLoadProjectFacts;
window.debouncedLoadProjectVulnerabilities = debouncedLoadProjectVulnerabilities;
window.loadProjectVulnerabilities = loadProjectVulnerabilities;
window.linkFactToExistingVulnerability = linkFactToExistingVulnerability;
window.createVulnerabilityFromCurrentFact = createVulnerabilityFromCurrentFact;
window.viewFactsForVulnerability = viewFactsForVulnerability;
window.openProjectConversation = openProjectConversation;
window.unbindConversationFromProject = unbindConversationFromProject;
window.loadProjectConversations = loadProjectConversations;
window.rebuildProjectNameMap = rebuildProjectNameMap;
window.projectNameById = projectNameById;
