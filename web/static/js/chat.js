let currentConversationId = null;
let loadConversationRequestSeq = 0;

// Internal UI state handling.
let mentionTools = [];
let mentionToolsLoaded = false;
let mentionToolsLoadingPromise = null;
let mentionSuggestionsEl = null;
let mentionFilteredTools = [];
let externalMcpNames = []; // External MCP name list
const mentionState = {
    active: false,
    startIndex: -1,
    query: '',
    selectedIndex: 0,
};

// Internal UI state handling.
let isComposing = false;

// Internal UI state handling.
const DRAFT_STORAGE_KEY = 'cyberstrike-chat-draft';
let draftSaveTimer = null;
const DRAFT_SAVE_DELAY = 500; // 500 ms debounce delay

// Internal UI state handling.
const MAX_CHAT_FILES = 10;
const CHAT_FILE_DEFAULT_PROMPT = 'Please analyze the uploaded file contents.';
/** Internal UI state handling. */
const CHAT_INTERRUPT_CONTINUE_USER_PREFIX = '[User supplement / continue after interruption]';
function isInterruptContinueInjectChatMessage(content) {
    return typeof content === 'string' && content.trimStart().startsWith(CHAT_INTERRUPT_CONTINUE_USER_PREFIX);
}
/**
 * Internal UI state handling.
 * @type {{ id: number, fileName: string, mimeType: string, serverPath: string|null, uploading: boolean, uploadPercent: number, uploadPromise: Promise<void>|null, uploadError: string|null }[]}
 */
let chatAttachments = [];
let chatAttachmentSeq = 0;

// Internal UI state handling.
const AGENT_MODE_STORAGE_KEY = 'cyberstrike-chat-agent-mode';
const REASONING_MODE_LS = 'cyberstrike-chat-reasoning-mode';
const REASONING_EFFORT_LS = 'cyberstrike-chat-reasoning-effort';
const CHAT_AGENT_MODE_REACT = 'react';
const CHAT_AGENT_MODE_EINO_SINGLE = 'eino_single';
const CHAT_AGENT_EINO_MODES = ['deep', 'plan_execute', 'supervisor'];
let multiAgentAPIEnabled = false;

// Internal UI state handling.
const HITL_STORAGE_PREFIX = 'cyberstrike-chat-hitl';
const HITL_DRAFT_KEY = 'cyberstrike-chat-hitl-draft';
/** Internal UI state handling. */
const HITL_GLOBAL_LAST_KEY = `${HITL_STORAGE_PREFIX}:__last__`;
const HITL_MODE_OFF = 'off';
const HITL_MODE_APPROVAL = 'approval';
const HITL_MODE_REVIEW_EDIT = 'review_edit';
const HITL_MODE_OPTIONS = [HITL_MODE_OFF, HITL_MODE_APPROVAL, HITL_MODE_REVIEW_EDIT];
let hitlApplyFeedbackTimer = null;

/** Internal UI state handling. */
function showChatToast(message, type) {
    const text = message == null ? '' : String(message);
    if (!text) return;
    const el = document.createElement('div');
    el.className = 'chat-files-toast' + (type === 'error' ? ' chat-toast--error' : '');
    el.setAttribute('role', 'status');
    el.textContent = text;
    document.body.appendChild(el);
    requestAnimationFrame(function () {
        el.classList.add('chat-files-toast-visible');
    });
    const hideMs = type === 'error' ? 4500 : 2600;
    setTimeout(function () {
        el.classList.remove('chat-files-toast-visible');
        setTimeout(function () { el.remove(); }, 300);
    }, hideMs);
}
if (typeof window !== 'undefined') {
    window.showChatToast = showChatToast;
}

function normalizeOrchestrationClient(s) {
    const v = String(s || '').trim().toLowerCase().replace(/-/g, '_');
    if (v === 'plan_execute' || v === 'planexecute' || v === 'pe') return 'plan_execute';
    if (v === 'supervisor' || v === 'super' || v === 'sv') return 'supervisor';
    return 'deep';
}

function chatAgentModeIsEino(mode) {
    return CHAT_AGENT_EINO_MODES.indexOf(mode) >= 0;
}

function chatAgentModeIsEinoSingle(mode) {
    return mode === CHAT_AGENT_MODE_EINO_SINGLE;
}

function normalizeHitlMode(mode) {
    let v = String(mode || '').trim().toLowerCase().replace(/-/g, '_');
    if (v === 'feedback' || v === 'followup') {
        v = HITL_MODE_APPROVAL;
    }
    if (HITL_MODE_OPTIONS.includes(v)) return v;
    return HITL_MODE_OFF;
}

function defaultHitlConfig() {
    return {
        mode: HITL_MODE_OFF,
        sensitiveTools: '',
        updatedAt: ''
    };
}

/** Internal UI state handling. */
function hitlToolsSplitToArray(s) {
    return String(s || '')
        .split(/[,\n\r]+/)
        .map(function (x) { return x.trim(); })
        .filter(Boolean);
}

/** Internal UI state handling. */
function hitlMergeToolsForDisplay(globalArr, sessionToolsArr) {
    const seen = Object.create(null);
    const out = [];
    function addOne(t) {
        const n = String(t || '').trim();
        if (!n) return;
        const k = n.toLowerCase();
        if (seen[k]) return;
        seen[k] = true;
        out.push(n);
    }
    if (Array.isArray(globalArr)) {
        globalArr.forEach(addOne);
    }
    if (Array.isArray(sessionToolsArr)) {
        sessionToolsArr.forEach(addOne);
    }
    return out.join(', ');
}

/** Internal UI state handling. */
function hitlStripGlobalToolsFromFormString(globalArr, commaStr) {
    if (!Array.isArray(globalArr) || globalArr.length === 0) {
        return typeof commaStr === 'string' ? commaStr.trim() : '';
    }
    const g = Object.create(null);
    globalArr.forEach(function (t) {
        const k = String(t || '').trim().toLowerCase();
        if (k) g[k] = true;
    });
    return hitlToolsSplitToArray(commaStr)
        .filter(function (p) {
            return p && !g[p.toLowerCase()];
        })
        .join(', ');
}

function getHitlStorageKeyByConversation(conversationId) {
    return `${HITL_STORAGE_PREFIX}:${String(conversationId || '').trim()}`;
}

function getHitlModeLabel(mode) {
    const safeMode = normalizeHitlMode(mode);
    if (typeof window.t === 'function') {
        switch (safeMode) {
            case HITL_MODE_APPROVAL:
                return window.t('chat.hitlModeApproval');
            case HITL_MODE_REVIEW_EDIT:
                return window.t('chat.hitlModeReviewEdit');
            default:
                return window.t('chat.hitlModeOff');
        }
    }
    return safeMode;
}

function getHitlLastGlobalConfig() {
    const fallback = defaultHitlConfig();
    try {
        const raw = localStorage.getItem(HITL_GLOBAL_LAST_KEY);
        if (!raw) return null;
        const parsed = JSON.parse(raw);
        if (!parsed || typeof parsed !== 'object') return null;
        return {
            mode: normalizeHitlMode(parsed.mode),
            sensitiveTools: typeof parsed.sensitiveTools === 'string' ? parsed.sensitiveTools : fallback.sensitiveTools,
            updatedAt: typeof parsed.updatedAt === 'string' ? parsed.updatedAt : ''
        };
    } catch (e) {
        return null;
    }
}

function saveHitlLastGlobalConfig(payload) {
    if (!payload || typeof payload !== 'object') return;
    try {
        localStorage.setItem(HITL_GLOBAL_LAST_KEY, JSON.stringify(payload));
    } catch (e) {
        console.warn('saveHitlLastGlobalConfig failed', e);
    }
}

function getHitlConfigForConversation(conversationId) {
    const fallback = defaultHitlConfig();
    const cid = conversationId ? String(conversationId).trim() : '';
    if (!cid) {
        const globalLast = getHitlLastGlobalConfig();
        let draftCfg = null;
        try {
            const raw = localStorage.getItem(HITL_DRAFT_KEY);
            if (raw) {
                const parsed = JSON.parse(raw);
                if (parsed && typeof parsed === 'object') {
                    draftCfg = {
                        mode: normalizeHitlMode(parsed.mode),
                        sensitiveTools: typeof parsed.sensitiveTools === 'string' ? parsed.sensitiveTools : fallback.sensitiveTools,
                        updatedAt: typeof parsed.updatedAt === 'string' ? parsed.updatedAt : ''
                    };
                }
            }
        } catch (e) {
            draftCfg = null;
        }
        const g = globalLast ? {
            mode: normalizeHitlMode(globalLast.mode),
            sensitiveTools: typeof globalLast.sensitiveTools === 'string' ? globalLast.sensitiveTools : fallback.sensitiveTools,
            updatedAt: typeof globalLast.updatedAt === 'string' ? globalLast.updatedAt : ''
        } : null;
        if (!draftCfg && !g) return fallback;
        if (!draftCfg) return g;
        if (!g) return draftCfg;
        const tg = Date.parse(g.updatedAt) || 0;
        const td = Date.parse(draftCfg.updatedAt) || 0;
        return tg > td ? g : draftCfg;
    }
    const key = getHitlStorageKeyByConversation(cid);
    try {
        const raw = localStorage.getItem(key);
        if (!raw) {
            return getHitlLastGlobalConfig() || fallback;
        }
        const parsed = JSON.parse(raw);
        if (!parsed || typeof parsed !== 'object') {
            return getHitlLastGlobalConfig() || fallback;
        }
        return {
            mode: normalizeHitlMode(parsed.mode),
            sensitiveTools: typeof parsed.sensitiveTools === 'string' ? parsed.sensitiveTools : fallback.sensitiveTools,
            updatedAt: typeof parsed.updatedAt === 'string' ? parsed.updatedAt : ''
        };
    } catch (e) {
        return getHitlLastGlobalConfig() || fallback;
    }
}

function saveHitlConfigForConversation(conversationId, cfg, opts) {
    const syncGlobalLast = !!(opts && opts.syncGlobalLast);
    const payload = {
        mode: normalizeHitlMode(cfg && cfg.mode),
        sensitiveTools: typeof (cfg && cfg.sensitiveTools) === 'string' ? cfg.sensitiveTools : '',
        updatedAt: typeof (cfg && cfg.updatedAt) === 'string' ? cfg.updatedAt : ''
    };
    const key = conversationId ? getHitlStorageKeyByConversation(conversationId) : HITL_DRAFT_KEY;
    try {
        localStorage.setItem(key, JSON.stringify(payload));
        if (syncGlobalLast) {
            saveHitlLastGlobalConfig(payload);
        }
    } catch (e) {
        console.warn('saveHitlConfigForConversation failed', e);
    }
}

function readHitlConfigFromForm() {
    const modeEl = document.getElementById('hitl-mode-select');
    const toolsEl = document.getElementById('hitl-sensitive-tools');
    const mode = normalizeHitlMode(modeEl ? modeEl.value : HITL_MODE_OFF);
    let sensitiveTools = toolsEl ? String(toolsEl.value || '').trim() : '';
    const g = typeof window !== 'undefined' ? window.csaiHitlGlobalToolWhitelist : null;
    if (Array.isArray(g) && g.length > 0) {
        sensitiveTools = hitlStripGlobalToolsFromFormString(g, sensitiveTools);
    }
    return {
        mode,
        sensitiveTools,
        updatedAt: new Date().toISOString()
    };
}

function updateHitlStatusUI(_cfg) {
    /* Internal UI state handling. */
}

function applyHitlConfigToUI(cfg) {
    const conf = cfg || defaultHitlConfig();
    const modeEl = document.getElementById('hitl-mode-select');
    const toolsEl = document.getElementById('hitl-sensitive-tools');
    if (modeEl) modeEl.value = normalizeHitlMode(conf.mode);
    let toolsVal = conf.sensitiveTools || '';
    const g = typeof window !== 'undefined' ? window.csaiHitlGlobalToolWhitelist : null;
    if (Array.isArray(g) && g.length > 0) {
        const sessionArr = hitlToolsSplitToArray(toolsVal);
        toolsVal = hitlMergeToolsForDisplay(g, sessionArr);
    }
    if (toolsEl) toolsEl.value = toolsVal;
    updateHitlStatusUI(conf);
}

function refreshHitlConfigByCurrentConversation() {
    const cfg = getHitlConfigForConversation(currentConversationId || '');
    applyHitlConfigToUI(cfg);
}

function showHitlApplyFeedback(text, isError, partial) {
    const el = document.getElementById('hitl-apply-feedback');
    if (hitlApplyFeedbackTimer) {
        clearTimeout(hitlApplyFeedbackTimer);
        hitlApplyFeedbackTimer = null;
    }
    if (!el) {
        if (text && isError) {
            showChatToast(text, 'error');
        }
        return;
    }
    el.classList.toggle('hitl-apply-feedback--error', !!isError);
    el.classList.toggle('hitl-apply-feedback--partial', !!partial && !isError);
    if (!text) {
        el.textContent = '';
        el.style.display = 'none';
        el.classList.remove('hitl-apply-feedback--error', 'hitl-apply-feedback--partial');
        return;
    }
    el.textContent = text;
    el.style.display = 'block';
    if (!isError) {
        hitlApplyFeedbackTimer = setTimeout(function () {
            el.textContent = '';
            el.style.display = 'none';
            el.classList.remove('hitl-apply-feedback--error');
            el.classList.remove('hitl-apply-feedback--partial');
            hitlApplyFeedbackTimer = null;
        }, 3200);
    }
}

/** Internal UI state handling. */
async function applyHitlSidebarConfig() {
    const btn = document.getElementById('hitl-apply-btn');
    showHitlApplyFeedback('', false);
    if (btn) btn.disabled = true;
    try {
        const cfg = readHitlConfigFromForm();
        const cid = typeof currentConversationId === 'string' ? currentConversationId.trim() : '';
        saveHitlConfigForConversation(cid, cfg, { syncGlobalLast: true });

        const toolsArr = hitlToolsSplitToArray(cfg.sensitiveTools || '');

        let yamlMerged = false;
        if (!cid && toolsArr.length > 0 && typeof window.mergeHitlGlobalToolWhitelist === 'function') {
            const newGlobal = await window.mergeHitlGlobalToolWhitelist(toolsArr);
            if (Array.isArray(newGlobal)) {
                window.csaiHitlGlobalToolWhitelist = newGlobal;
            }
            yamlMerged = true;
        }

        applyHitlConfigToUI(cfg);

        if (cid && typeof window.saveHitlConversationConfig === 'function') {
            await window.saveHitlConversationConfig(cid, cfg);
            const ok = typeof window.t === 'function' ? window.t('chat.hitlApplyOkSync') : 'Human-in-the-loop configuration saved and synced to the server.';
            showHitlApplyFeedback(ok, false);
        } else if (yamlMerged) {
            const okYaml = typeof window.t === 'function' ? window.t('chat.hitlApplyOkWhitelistYaml') : 'Saved to config.yaml. Click Apply again after selecting a conversation to sync session settings.';
            showHitlApplyFeedback(okYaml, false);
        } else {
            const localOnly = typeof window.t === 'function' ? window.t('chat.hitlApplyOkLocal') : 'Saved locally.';
            showHitlApplyFeedback(localOnly, false);
        }
    } catch (e) {
        console.warn('applyHitlSidebarConfig', e);
        const prefix = typeof window.t === 'function' ? window.t('chat.hitlApplyFail') : 'Save failed';
        const detail = (e && e.message) ? e.message : String(e);
        showHitlApplyFeedback(prefix + (detail ? ':' + detail : ''), true);
    } finally {
        if (btn) btn.disabled = false;
    }
}

/** Internal UI state handling. */
function chatAgentModeNormalizeStored(stored, cfg) {
    const pub = cfg && cfg.multi_agent ? cfg.multi_agent : null;
    const multiOn = !!(pub && pub.enabled);
    const defOrch = 'deep';
    let s = stored;
    if (s === 'single') s = CHAT_AGENT_MODE_REACT;
    if (s === 'multi') s = defOrch;
    if (s === CHAT_AGENT_MODE_REACT || chatAgentModeIsEinoSingle(s)) return s;
    if (chatAgentModeIsEino(s)) {
        return multiOn ? s : CHAT_AGENT_MODE_REACT;
    }
    return CHAT_AGENT_MODE_REACT;
}

if (typeof window !== 'undefined') {
    window.csaiHitlGlobalToolWhitelist = window.csaiHitlGlobalToolWhitelist || [];
    window.csaiChatAgentMode = {
        EINO_MODES: CHAT_AGENT_EINO_MODES,
        EINO_SINGLE: CHAT_AGENT_MODE_EINO_SINGLE,
        REACT: CHAT_AGENT_MODE_REACT,
        isEino: chatAgentModeIsEino,
        isEinoSingle: chatAgentModeIsEinoSingle,
        normalizeStored: chatAgentModeNormalizeStored,
        normalizeOrchestration: normalizeOrchestrationClient
    };
    window.applyHitlSidebarConfig = applyHitlSidebarConfig;
    window.readHitlConfigFromForm = readHitlConfigFromForm;
    window.applyHitlConfigToUI = applyHitlConfigToUI;
    window.saveHitlConfigForConversation = saveHitlConfigForConversation;
    window.getHitlLastGlobalConfig = getHitlLastGlobalConfig;
    window.hitlMergeToolsForDisplay = hitlMergeToolsForDisplay;
    window.hitlStripGlobalToolsFromFormString = hitlStripGlobalToolsFromFormString;
    window.hitlToolsSplitToArray = hitlToolsSplitToArray;
    window.updateHitlStatusUI = updateHitlStatusUI;
}

function toggleHitlSidebarCard() {
    var card = document.getElementById('hitl-sidebar-card');
    if (!card) return;
    card.classList.toggle('hitl-sidebar-collapsed');
    try {
        localStorage.setItem('hitl-sidebar-collapsed', card.classList.contains('hitl-sidebar-collapsed') ? '1' : '0');
    } catch (e) {}
}
window.toggleHitlSidebarCard = toggleHitlSidebarCard;

document.addEventListener('DOMContentLoaded', function () {
    var card = document.getElementById('hitl-sidebar-card');
    if (card && localStorage.getItem('hitl-sidebar-collapsed') === '0') {
        card.classList.remove('hitl-sidebar-collapsed');
    }
});

function getAgentModeLabelForValue(mode) {
    if (typeof window.t === 'function') {
        switch (mode) {
            case CHAT_AGENT_MODE_REACT:
                return window.t('chat.agentModeReactNative');
            case 'deep':
                return window.t('chat.agentModeDeep');
            case 'plan_execute':
                return window.t('chat.agentModePlanExecuteLabel');
            case 'supervisor':
                return window.t('chat.agentModeSupervisorLabel');
            case CHAT_AGENT_MODE_EINO_SINGLE:
                return window.t('chat.agentModeEinoSingle');
            default:
                return mode;
        }
    }
    switch (mode) {
        case CHAT_AGENT_MODE_REACT: return 'Native ReAct';
        case CHAT_AGENT_MODE_EINO_SINGLE: return 'Eino Single agent';
        case 'deep': return 'Deep';
        case 'plan_execute': return 'Plan-Execute';
        case 'supervisor': return 'Supervisor';
        default: return mode;
    }
}

function getAgentModeIconForValue(mode) {
    switch (mode) {
        case CHAT_AGENT_MODE_REACT: return '🤖';
        case CHAT_AGENT_MODE_EINO_SINGLE: return '⚡';
        case 'deep': return '🧩';
        case 'plan_execute': return '📋';
        case 'supervisor': return '🎯';
        default: return '🤖';
    }
}

function syncAgentModeFromValue(value) {
    const hid = document.getElementById('agent-mode-select');
    const label = document.getElementById('agent-mode-text');
    const icon = document.getElementById('agent-mode-icon');
    if (hid) hid.value = value;
    if (label) label.textContent = getAgentModeLabelForValue(value);
    if (icon) icon.textContent = getAgentModeIconForValue(value);
    document.querySelectorAll('.agent-mode-option').forEach(function (el) {
        const v = el.getAttribute('data-value');
        el.classList.toggle('selected', v === value);
    });
    syncReasoningRowVisibility(value);
}

function syncReasoningRowVisibility(modeVal) {
    const wrap = document.getElementById('chat-reasoning-wrapper');
    if (!wrap) return;
    const show = modeVal === CHAT_AGENT_MODE_EINO_SINGLE || (multiAgentAPIEnabled && chatAgentModeIsEino(modeVal));
    wrap.style.display = show ? '' : 'none';
    if (!show) {
        closeChatReasoningPanel();
    } else {
        updateChatReasoningSummary();
    }
}

function reasoningSummaryModeLabel(mode) {
    const m = (mode || 'default').trim();
    const t = (typeof window.t === 'function') ? window.t : function (k) { return k; };
    switch (m) {
        case 'off': return t('chat.reasoningModeOff');
        case 'on': return t('chat.reasoningModeOn');
        case 'auto': return t('chat.reasoningModeAuto');
        default: return t('chat.reasoningSummaryFollow');
    }
}

function updateChatReasoningSummary() {
    const el = document.getElementById('chat-reasoning-summary');
    const modeEl = document.getElementById('chat-reasoning-mode');
    const effEl = document.getElementById('chat-reasoning-effort');
    if (!el || !modeEl) return;
    const mode = (modeEl.value || 'default').trim();
    const effort = effEl && effEl.value ? String(effEl.value).trim() : '';
    const t = (typeof window.t === 'function') ? window.t : function (k) { return k; };
    const modePart = reasoningSummaryModeLabel(mode);
    const effPart = effort || t('chat.reasoningSummaryDash');
    el.textContent = modePart + ' / ' + effPart;
}

function closeChatReasoningPanel() {
    const wrap = document.getElementById('chat-reasoning-wrapper');
    const toggle = document.getElementById('conversation-reasoning-toggle');
    if (wrap) wrap.classList.add('conversation-reasoning-collapsed');
    if (toggle) toggle.setAttribute('aria-expanded', 'false');
}

function toggleConversationReasoningCard() {
    const wrap = document.getElementById('chat-reasoning-wrapper');
    const toggle = document.getElementById('conversation-reasoning-toggle');
    if (!wrap || !toggle) return;
    wrap.classList.toggle('conversation-reasoning-collapsed');
    const collapsed = wrap.classList.contains('conversation-reasoning-collapsed');
    toggle.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
    if (!collapsed) {
        if (typeof closeAgentModePanel === 'function') {
            closeAgentModePanel();
        }
        if (typeof closeRoleSelectionPanel === 'function') {
            closeRoleSelectionPanel();
        }
        updateChatReasoningSummary();
    }
}

function toggleChatReasoningPanel() {
    toggleConversationReasoningCard();
}

function restoreChatReasoningControlsFromStorage() {
    try {
        const m = document.getElementById('chat-reasoning-mode');
        const e = document.getElementById('chat-reasoning-effort');
        if (m) {
            const v = localStorage.getItem(REASONING_MODE_LS);
            if (v && ['default', 'off', 'on', 'auto'].indexOf(v) !== -1) {
                m.value = v;
            }
        }
        if (e) {
            const v = localStorage.getItem(REASONING_EFFORT_LS);
            if (v !== null && ['', 'low', 'medium', 'high', 'max', 'xhigh'].indexOf(v) !== -1) {
                e.value = v;
            }
        }
        updateChatReasoningSummary();
    } catch (err) { /* ignore */ }
}

function persistChatReasoningPrefs() {
    try {
        const m = document.getElementById('chat-reasoning-mode');
        const elEff = document.getElementById('chat-reasoning-effort');
        if (m) localStorage.setItem(REASONING_MODE_LS, m.value || 'default');
        if (elEff) localStorage.setItem(REASONING_EFFORT_LS, elEff.value || '');
        updateChatReasoningSummary();
    } catch (err) { /* ignore */ }
}

/** Internal UI state handling. */
function buildReasoningRequestPayload() {
    const wrap = document.getElementById('chat-reasoning-wrapper');
    if (!wrap || wrap.style.display === 'none') {
        return undefined;
    }
    const modeEl = document.getElementById('chat-reasoning-mode');
    const effEl = document.getElementById('chat-reasoning-effort');
    if (!modeEl) return undefined;
    const mode = (modeEl.value || 'default').trim();
    const effort = effEl && effEl.value ? String(effEl.value).trim() : '';
    if (mode === 'default' && !effort) {
        return undefined;
    }
    const o = {};
    if (mode !== 'default') o.mode = mode;
    if (effort) o.effort = effort;
    return Object.keys(o).length ? o : undefined;
}

if (typeof window !== 'undefined') {
    window.persistChatReasoningPrefs = persistChatReasoningPrefs;
    window.buildReasoningRequestPayload = buildReasoningRequestPayload;
    window.closeChatReasoningPanel = closeChatReasoningPanel;
    window.toggleChatReasoningPanel = toggleChatReasoningPanel;
    window.toggleConversationReasoningCard = toggleConversationReasoningCard;
    window.updateChatReasoningSummary = updateChatReasoningSummary;
}

function closeAgentModePanel() {
    const panel = document.getElementById('agent-mode-panel');
    const btn = document.getElementById('agent-mode-btn');
    if (panel) panel.style.display = 'none';
    if (btn) {
        btn.classList.remove('active');
        btn.setAttribute('aria-expanded', 'false');
    }
}

function toggleAgentModePanel() {
    const panel = document.getElementById('agent-mode-panel');
    const btn = document.getElementById('agent-mode-btn');
    if (!panel || !btn) return;
    const isOpen = panel.style.display === 'flex';
    if (isOpen) {
        closeAgentModePanel();
        return;
    }
    if (typeof closeChatReasoningPanel === 'function') {
        closeChatReasoningPanel();
    }
    if (typeof closeRoleSelectionPanel === 'function') {
        closeRoleSelectionPanel();
    }
    if (typeof closeChatProjectPanel === 'function') {
        closeChatProjectPanel();
    }
    panel.style.display = 'flex';
    btn.classList.add('active');
    btn.setAttribute('aria-expanded', 'true');
}

function selectAgentMode(mode) {
    const ok = mode === CHAT_AGENT_MODE_REACT || chatAgentModeIsEinoSingle(mode) || chatAgentModeIsEino(mode);
    if (!ok) return;
    try {
        localStorage.setItem(AGENT_MODE_STORAGE_KEY, mode);
    } catch (e) { /* ignore */ }
    syncAgentModeFromValue(mode);
    closeAgentModePanel();
}

async function initChatAgentModeFromConfig() {
    const wrap = document.getElementById('agent-mode-wrapper');
    const sel = document.getElementById('agent-mode-select');
    if (!wrap || !sel) return;

    // Internal UI state handling.
    wrap.style.display = '';
    let stored = localStorage.getItem(AGENT_MODE_STORAGE_KEY);
    if (!(stored === CHAT_AGENT_MODE_REACT || chatAgentModeIsEinoSingle(stored) || chatAgentModeIsEino(stored))) {
        stored = CHAT_AGENT_MODE_REACT;
    }
    sel.value = stored;
    syncAgentModeFromValue(stored);
    document.querySelectorAll('.agent-mode-option').forEach(function (el) {
        const v = el.getAttribute('data-value');
        if (v === 'deep' || v === 'plan_execute' || v === 'supervisor') {
            el.style.display = 'none';
        } else {
            el.style.display = '';
        }
    });
    restoreChatReasoningControlsFromStorage();
    syncReasoningRowVisibility(stored);

    try {
        const r = await apiFetch('/api/config');
        if (!r.ok) return;
        const cfg = await r.json();
        multiAgentAPIEnabled = !!(cfg.multi_agent && cfg.multi_agent.enabled);
        if (typeof window !== 'undefined') {
            window.__csaiMultiAgentPublic = cfg.multi_agent || null;
            const tw = cfg.hitl && cfg.hitl.tool_whitelist;
            if (Array.isArray(tw)) {
                window.csaiHitlGlobalToolWhitelist = tw.slice();
            }
        }
        document.querySelectorAll('.agent-mode-option').forEach(function (el) {
            const v = el.getAttribute('data-value');
            if (v === 'deep' || v === 'plan_execute' || v === 'supervisor') {
                el.style.display = multiAgentAPIEnabled ? '' : 'none';
            } else {
                el.style.display = '';
            }
        });
        stored = chatAgentModeNormalizeStored(stored, cfg);
        try {
            localStorage.setItem(AGENT_MODE_STORAGE_KEY, stored);
        } catch (e) { /* ignore */ }
        sel.value = stored;
        syncAgentModeFromValue(stored);
        restoreChatReasoningControlsFromStorage();
        syncReasoningRowVisibility(stored);
    } catch (e) {
        console.warn('initChatAgentModeFromConfig', e);
    }
}

document.addEventListener('languagechange', function () {
    const hid = document.getElementById('agent-mode-select');
    if (!hid) return;
    const v = hid.value;
    if (v === CHAT_AGENT_MODE_REACT || chatAgentModeIsEinoSingle(v) || chatAgentModeIsEino(v)) {
        syncAgentModeFromValue(v);
    }
    if (typeof updateChatReasoningSummary === 'function') {
        updateChatReasoningSummary();
    }
});

// Internal UI state handling.
function saveChatDraftDebounced(content) {
    // Internal UI state handling.
    if (draftSaveTimer) {
        clearTimeout(draftSaveTimer);
    }
    
    // Internal UI state handling.
    draftSaveTimer = setTimeout(() => {
        saveChatDraft(content);
    }, DRAFT_SAVE_DELAY);
}

// Internal UI state handling.
function saveChatDraft(content) {
    try {
        const chatInput = document.getElementById('chat-input');
        const placeholderText = chatInput ? (chatInput.getAttribute('placeholder') || '').trim() : '';
        const trimmed = (content || '').trim();

        // Internal UI state handling.
        if (trimmed && (!placeholderText || trimmed !== placeholderText)) {
            localStorage.setItem(DRAFT_STORAGE_KEY, content);
        } else {
            // Internal UI state handling.
            localStorage.removeItem(DRAFT_STORAGE_KEY);
        }
    } catch (error) {
        // Internal UI state handling.
        console.warn('Save failed:', error);
    }
}

// Internal UI state handling.
function restoreChatDraft() {
    try {
        const chatInput = document.getElementById('chat-input');
        if (!chatInput) {
            return;
        }
        const placeholderText = (chatInput.getAttribute('placeholder') || '').trim();
        // Internal UI state handling.
        if (placeholderText && chatInput.value.trim() === placeholderText) {
            chatInput.value = '';
        }
        // Internal UI state handling.
        if (chatInput.value && chatInput.value.trim().length > 0) {
            return;
        }
        
        const draft = localStorage.getItem(DRAFT_STORAGE_KEY);
        const trimmedDraft = draft ? draft.trim() : '';

        // Internal UI state handling.
        if (trimmedDraft && (!placeholderText || trimmedDraft !== placeholderText)) {
            chatInput.value = draft;
            // Internal UI state handling.
            adjustTextareaHeight(chatInput);
        } else if (trimmedDraft && placeholderText && trimmedDraft === placeholderText) {
            // Internal UI state handling.
            localStorage.removeItem(DRAFT_STORAGE_KEY);
        }
    } catch (error) {
        console.warn('Failed:', error);
    }
}

// Internal UI state handling.
function clearChatDraft() {
    try {
        // Internal UI state handling.
        localStorage.removeItem(DRAFT_STORAGE_KEY);
    } catch (error) {
        console.warn('Failed:', error);
    }
}

// Internal UI state handling.
function adjustTextareaHeight(textarea) {
    if (!textarea) return;
    
    // Internal UI state handling.
    textarea.style.height = 'auto';
    // Internal UI state handling.
    void textarea.offsetHeight;
    
    // Internal UI state handling.
    const scrollHeight = textarea.scrollHeight;
    const newHeight = Math.min(Math.max(scrollHeight, 40), 300);
    textarea.style.height = newHeight + 'px';
    
    // Internal UI state handling.
    if (!textarea.value || textarea.value.trim().length === 0) {
        textarea.style.height = '40px';
    }
}

// Internal UI state handling.
async function sendMessage() {
    const input = document.getElementById('chat-input');
    let message = input.value.trim();
    const hasAttachments = chatAttachments && chatAttachments.length > 0;

    if (!message && !hasAttachments) {
        return;
    }

    if (hasAttachments) {
        const needWait = chatAttachments.some((a) => a.uploading);
        if (needWait) {
            const waitLabel = (typeof window.t === 'function')
                ? window.t('chat.waitingAttachmentsUpload')
                : 'Loading…';
            chatAttachmentProgressSet(true, 0, waitLabel);
        }
        try {
            await Promise.all(chatAttachments.map((a) => (a.uploadPromise ? a.uploadPromise : Promise.resolve())));
        } finally {
            refreshChatAttachmentUploadProgress();
        }
        const bad = chatAttachments.filter((a) => !a.serverPath);
        if (bad.length) {
            const hint = (typeof window.t === 'function')
                ? window.t('chat.attachmentsUploadIncomplete')
                : 'Some files failed to upload. Choose files again, then send.';
            alert(hint);
            return;
        }
    }

    // Internal UI state handling.
    if (hasAttachments && !message) {
        message = CHAT_FILE_DEFAULT_PROMPT;
    }

    // Internal UI state handling.
    const displayMessage = hasAttachments
        ? message + '\n' + chatAttachments.map(a => '📎 ' + a.fileName).join('\n')
        : message;
    if (window.CyberStrikeChatScroll) {
        window.CyberStrikeChatScroll.onUserSendMessage();
    }
    addMessage('user', displayMessage, null, null, null, { scroll: 'none' });
    
    // Internal UI state handling.
    if (draftSaveTimer) {
        clearTimeout(draftSaveTimer);
        draftSaveTimer = null;
    }
    
    // Internal UI state handling.
    clearChatDraft();
    // Internal UI state handling.
    try {
        localStorage.removeItem(DRAFT_STORAGE_KEY);
    } catch (e) {
        // Internal UI state handling.
    }
    
    // Internal UI state handling.
    input.value = '';
    // Internal UI state handling.
    input.style.height = '40px';

    // Internal UI state handling.
    const body = {
        message: message,
        conversationId: currentConversationId,
        role: typeof getCurrentRole === 'function' ? getCurrentRole() : ''
    };
    if (!currentConversationId && typeof getActiveProjectId === 'function') {
        const pid = getActiveProjectId();
        if (pid) body.projectId = pid;
    }
    const hitlCfg = readHitlConfigFromForm();
    if (normalizeHitlMode(hitlCfg.mode) !== HITL_MODE_OFF) {
        const sensitiveTools = hitlToolsSplitToArray(hitlCfg.sensitiveTools || '');
        body.hitl = {
            enabled: true,
            mode: normalizeHitlMode(hitlCfg.mode),
            sensitiveTools: sensitiveTools
        };
    }
    if (hasAttachments) {
        body.attachments = chatAttachments.map((a) => ({
            fileName: a.fileName,
            mimeType: a.mimeType || '',
            serverPath: a.serverPath
        }));
    }
    const reasoningPayload = buildReasoningRequestPayload();
    if (reasoningPayload) {
        body.reasoning = reasoningPayload;
    }
    // Internal UI state handling.
    chatAttachments = [];
    renderChatFileChips();

    // Internal UI state handling.
    const progressId = addProgressMessage();
    if (window.CyberStrikeChatScroll) {
        window.CyberStrikeChatScroll.markProgressStreaming(true, progressId);
        window.CyberStrikeChatScroll.onUserSendMessage();
    }
    const progressElement = document.getElementById(progressId);
    registerProgressTask(progressId, currentConversationId);
    loadActiveTasks();
    let assistantMessageId = null;
    let mcpExecutionIds = [];
    
    try {
        const modeSel = document.getElementById('agent-mode-select');
        const modeVal = modeSel ? modeSel.value : CHAT_AGENT_MODE_REACT;
        const useEinoSingle = chatAgentModeIsEinoSingle(modeVal);
        const useMulti = multiAgentAPIEnabled && chatAgentModeIsEino(modeVal);
        const streamPath = useEinoSingle ? '/api/eino-agent/stream' : useMulti ? '/api/multi-agent/stream' : '/api/agent-loop/stream';
        if (useMulti && modeVal) {
            body.orchestration = modeVal;
        }
        const response = await apiFetch(streamPath, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(body),
        });
        
        if (!response.ok) {
            throw new Error('RequestFailed: ' + response.status);
        }

        window.__csAgentLiveStream = {
            active: true,
            conversationId: currentConversationId || null,
            progressId: progressId
        };
        try {
            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop(); // Internal UI state handling.

                for (const line of lines) {
                    if (line.startsWith('data: ')) {
                        try {
                            const eventData = JSON.parse(line.slice(6));
                            handleStreamEvent(eventData, progressElement, progressId,
                                             () => assistantMessageId, (id) => { assistantMessageId = id; },
                                             () => mcpExecutionIds, (ids) => { mcpExecutionIds = ids; });
                        } catch (e) {
                            console.error('Event parsing failed:', e, line);
                        }
                    }
                }
            }
            // Flush decoder internal buffer to avoid losing the final partial UTF-8 code point.
            buffer += decoder.decode();

            // Internal UI state handling.
            if (buffer.trim()) {
                const lines = buffer.split('\n');
                for (const line of lines) {
                    if (line.startsWith('data: ')) {
                        try {
                            const eventData = JSON.parse(line.slice(6));
                            handleStreamEvent(eventData, progressElement, progressId,
                                             () => assistantMessageId, (id) => { assistantMessageId = id; },
                                             () => mcpExecutionIds, (ids) => { mcpExecutionIds = ids; });
                        } catch (e) {
                            console.error('Event parsing failed:', e, line);
                        }
                    }
                }
            }
        } finally {
            window.__csAgentLiveStream = { active: false, conversationId: null, progressId: null };
            if (window.CyberStrikeChatScroll) {
                window.CyberStrikeChatScroll.onStreamEnd();
            }
        }

        // Internal UI state handling.
        clearChatDraft();
        try {
            localStorage.removeItem(DRAFT_STORAGE_KEY);
        } catch (e) {
            // Internal UI state handling.
        }
        
    } catch (error) {
        removeMessage(progressId);
        const msg = error && error.message != null ? String(error.message) : String(error);
        const isNetwork = /network|fetch|Failed to fetch|aborted|AbortError|load failed|NetworkError/i.test(msg);
        if (isNetwork && typeof window.t === 'function') {
            addMessage('system', window.t('chat.streamNetworkErrorHint', { detail: msg }));
        } else if (isNetwork) {
            addMessage('system', 'Network error (' + msg + '). The task may still be running; check Running tasks before starting a new chat.');
        } else {
            addMessage('system', 'Error: ' + msg);
        }
        if (typeof loadActiveTasks === 'function') {
            loadActiveTasks();
        }
        // Internal UI state handling.
    }
}

// Internal UI state handling.
function renderChatFileChips() {
    const list = document.getElementById('chat-file-list');
    if (!list) return;
    list.innerHTML = '';
    if (!chatAttachments.length) return;
    chatAttachments.forEach((a, i) => {
        const chip = document.createElement('div');
        chip.className = 'chat-file-chip';
        if (a.uploading) chip.classList.add('chat-file-chip--uploading');
        if (a.uploadError) chip.classList.add('chat-file-chip--error');
        chip.setAttribute('role', 'listitem');
        const name = document.createElement('span');
        name.className = 'chat-file-chip-name';
        name.title = a.fileName;
        let label = a.fileName;
        if (a.uploading) {
            label += ' · ' + ((typeof window.t === 'function') ? window.t('chat.attachmentUploading') : 'Loading…');
        } else if (a.uploadError) {
            label += ' · ' + ((typeof window.t === 'function') ? window.t('chat.attachmentUploadFailed') : 'Failed');
        }
        name.textContent = label;
        const remove = document.createElement('button');
        remove.type = 'button';
        remove.className = 'chat-file-chip-remove';
        remove.title = typeof window.t === 'function' ? window.t('chatGroup.remove') : 'Copied';
        remove.innerHTML = '×';
        remove.setAttribute('aria-label', 'Remove ' + a.fileName);
        remove.addEventListener('click', () => removeChatAttachment(i));
        chip.appendChild(name);
        chip.appendChild(remove);
        list.appendChild(chip);
    });
}

function removeChatAttachment(index) {
    chatAttachments.splice(index, 1);
    renderChatFileChips();
    refreshChatAttachmentUploadProgress();
}

// Internal UI state handling.
function appendChatFilePrompt() {
    const input = document.getElementById('chat-input');
    if (!input || !chatAttachments.length) return;
    if (!input.value.trim()) {
        input.value = CHAT_FILE_DEFAULT_PROMPT;
        adjustTextareaHeight(input);
    }
}

function chatAttachmentProgressSet(visible, percent, detailText) {
    const wrap = document.getElementById('chat-attachment-progress');
    const fill = document.getElementById('chat-attachment-progress-fill');
    const label = document.getElementById('chat-attachment-progress-label');
    if (!wrap || !fill || !label) return;
    if (!visible) {
        wrap.hidden = true;
        fill.style.width = '0%';
        label.textContent = '';
        return;
    }
    wrap.hidden = false;
    const p = Math.min(100, Math.max(0, Math.round(percent)));
    fill.style.width = p + '%';
    label.textContent = detailText || '';
}

function refreshChatAttachmentUploadProgress() {
    if (!chatAttachments.length) {
        chatAttachmentProgressSet(false);
        return;
    }
    const uploading = chatAttachments.filter((a) => a.uploading);
    if (!uploading.length) {
        chatAttachmentProgressSet(false);
        return;
    }
    let sum = 0;
    chatAttachments.forEach((a) => {
        sum += a.uploading ? (a.uploadPercent || 0) : 100;
    });
    const overall = Math.round(sum / chatAttachments.length);
    const line = (typeof window.t === 'function')
        ? window.t('chat.uploadingAttachmentsDetail', {
            done: chatAttachments.length - uploading.length,
            total: chatAttachments.length,
            percent: overall
        })
        : ('Uploading ' + (chatAttachments.length - uploading.length) + '/' + chatAttachments.length + ' · ' + overall + '%');
    chatAttachmentProgressSet(true, overall, line);
}

async function uploadOneChatAttachment(entry, file) {
    const form = new FormData();
    form.append('file', file);
    const conv = currentConversationId;
    if (conv && String(conv).trim()) {
        form.append('conversationId', String(conv).trim());
    }
    const entryId = entry.id;
    try {
        const res = typeof apiUploadWithProgress === 'function'
            ? await apiUploadWithProgress('/api/chat-uploads', form, {
                onProgress: function (p) {
                    const cur = chatAttachments.find((x) => x.id === entryId);
                    if (cur) {
                        cur.uploadPercent = p.percent;
                        refreshChatAttachmentUploadProgress();
                    }
                }
            })
            : await apiFetch('/api/chat-uploads', { method: 'POST', body: form });
        if (!res.ok) {
            throw new Error(await res.text());
        }
        const data = await res.json().catch(() => ({}));
        const abs = data.absolutePath ? String(data.absolutePath).trim() : '';
        if (!abs) {
            throw new Error('no absolutePath in response');
        }
        const cur = chatAttachments.find((x) => x.id === entryId);
        if (cur) {
            cur.serverPath = abs;
            cur.uploading = false;
            cur.uploadPercent = 100;
            cur.uploadError = null;
        }
    } catch (e) {
        const msg = (e && e.message) ? e.message : String(e);
        const cur = chatAttachments.find((x) => x.id === entryId);
        if (cur) {
            cur.uploading = false;
            cur.uploadError = msg;
            cur.serverPath = null;
        }
        alert(((typeof window.t === 'function') ? window.t('chat.attachmentUploadAlert', { name: file.name }) : ('Failed:' + file.name)) + '\n' + msg);
    }
    renderChatFileChips();
    refreshChatAttachmentUploadProgress();
}

async function addFilesToChat(files) {
    if (!files || !files.length) return;
    const next = Array.from(files);
    if (chatAttachments.length + next.length > MAX_CHAT_FILES) {
        alert('You can upload at most ' + MAX_CHAT_FILES + ' files; ' + chatAttachments.length + ' are already selected.');
        return;
    }
    next.forEach((file) => {
        const id = ++chatAttachmentSeq;
        const entry = {
            id: id,
            fileName: file.name,
            mimeType: file.type || '',
            serverPath: null,
            uploading: true,
            uploadPercent: 0,
            uploadPromise: null,
            uploadError: null
        };
        entry.uploadPromise = uploadOneChatAttachment(entry, file);
        chatAttachments.push(entry);
    });
    renderChatFileChips();
    refreshChatAttachmentUploadProgress();
    appendChatFilePrompt();
}

function setupChatFileUpload() {
    const inputEl = document.getElementById('chat-file-input');
    const container = document.getElementById('chat-input-container') || document.querySelector('.chat-input-container');
    if (!inputEl || !container) return;

    inputEl.addEventListener('change', function () {
        const files = this.files;
        if (files && files.length) {
            addFilesToChat(files).catch(function () { /* addFilesToChat text */ });
        }
        this.value = '';
    });

    container.addEventListener('dragover', function (e) {
        e.preventDefault();
        e.stopPropagation();
        this.classList.add('drag-over');
    });
    container.addEventListener('dragleave', function (e) {
        e.preventDefault();
        e.stopPropagation();
        if (!this.contains(e.relatedTarget)) {
            this.classList.remove('drag-over');
        }
    });
    container.addEventListener('drop', function (e) {
        e.preventDefault();
        e.stopPropagation();
        this.classList.remove('drag-over');
        const files = e.dataTransfer && e.dataTransfer.files;
        if (files && files.length) addFilesToChat(files).catch(function () { /* addFilesToChat text */ });
    });
}

// Internal UI state handling.
function ensureChatInputContainerId() {
    const c = document.querySelector('.chat-input-container');
    if (c && !c.id) c.id = 'chat-input-container';
}

function setupMentionSupport() {
    mentionSuggestionsEl = document.getElementById('mention-suggestions');
    if (mentionSuggestionsEl) {
        mentionSuggestionsEl.style.display = 'none';
        mentionSuggestionsEl.addEventListener('mousedown', (event) => {
            // Internal UI state handling.
            event.preventDefault();
        });
    }
    ensureMentionToolsLoaded().catch(() => {
        // Internal UI state handling.
    });
}

// Internal UI state handling.
function refreshMentionTools() {
    mentionToolsLoaded = false;
    mentionTools = [];
    externalMcpNames = [];
    mentionToolsLoadingPromise = null;
    // Internal UI state handling.
    if (mentionState.active) {
        ensureMentionToolsLoaded().catch(() => {
            // Internal UI state handling.
        });
    }
}

// Internal UI state handling.
if (typeof window !== 'undefined') {
    window.refreshMentionTools = refreshMentionTools;
}

function ensureMentionToolsLoaded() {
    // Internal UI state handling.
    if (typeof window !== 'undefined' && window._mentionToolsRoleChanged) {
        mentionToolsLoaded = false;
        mentionTools = [];
        delete window._mentionToolsRoleChanged;
    }
    
    if (mentionToolsLoaded) {
        return Promise.resolve(mentionTools);
    }
    if (mentionToolsLoadingPromise) {
        return mentionToolsLoadingPromise;
    }
    mentionToolsLoadingPromise = fetchMentionTools().finally(() => {
        mentionToolsLoadingPromise = null;
    });
    return mentionToolsLoadingPromise;
}

// Internal UI state handling.
function getToolKeyForMention(tool) {
    // Internal UI state handling.
    // Internal UI state handling.
    if (tool.is_external && tool.external_mcp) {
        return `${tool.external_mcp}::${tool.name}`;
    }
    return tool.name;
}

async function fetchMentionTools() {
    const pageSize = 100;
    let page = 1;
    let totalPages = 1;
    const seen = new Set();
    const collected = [];

    try {
        // Internal UI state handling.
        const roleName = typeof getCurrentRole === 'function' ? getCurrentRole() : '';

        // Internal UI state handling.
        try {
            const mcpResponse = await apiFetch('/api/external-mcp');
            if (mcpResponse.ok) {
                const mcpData = await mcpResponse.json();
                externalMcpNames = Object.keys(mcpData.servers || {}).filter(name => {
                    const server = mcpData.servers[name];
                    // Internal UI state handling.
                    return server.status === 'connected' && 
                           (server.config.external_mcp_enable || (server.config.enabled && !server.config.disabled));
                });
            }
        } catch (mcpError) {
            console.warn('textMCP load failed:', mcpError);
            externalMcpNames = [];
        }

        while (page <= totalPages && page <= 20) {
            // Internal UI state handling.
            let url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
            if (roleName && roleName !== 'Default') {
                url += `&role=${encodeURIComponent(roleName)}`;
            }

            const response = await apiFetch(url);
            if (!response.ok) {
                break;
            }
            const result = await response.json();
            const tools = Array.isArray(result.tools) ? result.tools : [];
            tools.forEach(tool => {
                if (!tool || !tool.name) {
                    return;
                }
                // Internal UI state handling.
                const toolKey = getToolKeyForMention(tool);
                if (seen.has(toolKey)) {
                    return;
                }
                seen.add(toolKey);

                // Internal UI state handling.
                // Internal UI state handling.
                // Internal UI state handling.
                let roleEnabled = tool.enabled !== false;
                if (tool.role_enabled !== undefined && tool.role_enabled !== null) {
                    roleEnabled = tool.role_enabled;
                }

                collected.push({
                    name: tool.name,
                    description: tool.description || '',
                    enabled: tool.enabled !== false, // toolsStatus
                    roleEnabled: roleEnabled, // Internal UI state handling.
                    isExternal: !!tool.is_external,
                    externalMcp: tool.external_mcp || '',
                    toolKey: toolKey, // Internal UI state handling.
                });
            });
            totalPages = result.total_pages || 1;
            page += 1;
            if (page > totalPages) {
                break;
            }
        }
        mentionTools = collected;
        mentionToolsLoaded = true;
    } catch (error) {
        console.warn('Failed to load tools for @mention:', error);
    }
    return mentionTools;
}

function handleChatInputInput(event) {
    const textarea = event.target;
    updateMentionStateFromInput(textarea);
    // Internal UI state handling.
    // Internal UI state handling.
    requestAnimationFrame(() => {
        adjustTextareaHeight(textarea);
    });
    // Internal UI state handling.
    saveChatDraftDebounced(textarea.value);
}

function handleChatInputClick(event) {
    updateMentionStateFromInput(event.target);
}

function handleChatInputKeydown(event) {
    // Internal UI state handling.
    // Internal UI state handling.
    if (event.isComposing || isComposing) {
        return;
    }

    if (mentionState.active && mentionSuggestionsEl && mentionSuggestionsEl.style.display !== 'none') {
        if (event.key === 'ArrowDown') {
            event.preventDefault();
            moveMentionSelection(1);
            return;
        }
        if (event.key === 'ArrowUp') {
            event.preventDefault();
            moveMentionSelection(-1);
            return;
        }
        if (event.key === 'Enter' || event.key === 'Tab') {
            event.preventDefault();
            applyMentionSelection();
            return;
        }
        if (event.key === 'Escape') {
            event.preventDefault();
            deactivateMentionState();
            return;
        }
    }

    if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        sendMessage();
    }
}

function updateMentionStateFromInput(textarea) {
    if (!textarea) {
        deactivateMentionState();
        return;
    }
    const caret = textarea.selectionStart || 0;
    const textBefore = textarea.value.slice(0, caret);
    const atIndex = textBefore.lastIndexOf('@');

    if (atIndex === -1) {
        deactivateMentionState();
        return;
    }

    // Internal UI state handling.
    if (atIndex > 0) {
        const boundaryChar = textBefore[atIndex - 1];
        if (boundaryChar && !/\s/.test(boundaryChar) && !'([{, .,.;:!?'.includes(boundaryChar)) {
            deactivateMentionState();
            return;
        }
    }

    const querySegment = textBefore.slice(atIndex + 1);

    if (querySegment.includes(' ') || querySegment.includes('\n') || querySegment.includes('\t') || querySegment.includes('@')) {
        deactivateMentionState();
        return;
    }

    if (querySegment.length > 60) {
        deactivateMentionState();
        return;
    }

    mentionState.active = true;
    mentionState.startIndex = atIndex;
    mentionState.query = querySegment.toLowerCase();
    mentionState.selectedIndex = 0;

    if (!mentionToolsLoaded) {
        renderMentionSuggestions({ showLoading: true });
    } else {
        updateMentionCandidates();
        renderMentionSuggestions();
    }

    ensureMentionToolsLoaded().then(() => {
        if (mentionState.active) {
            updateMentionCandidates();
            renderMentionSuggestions();
        }
    });
}

function updateMentionCandidates() {
    if (!mentionState.active) {
        mentionFilteredTools = [];
        return;
    }
    const normalizedQuery = (mentionState.query || '').trim().toLowerCase();
    let filtered = mentionTools;

    if (normalizedQuery) {
        // Internal UI state handling.
        const exactMatchedMcp = externalMcpNames.find(mcpName => 
            mcpName.toLowerCase() === normalizedQuery
        );

        if (exactMatchedMcp) {
            // Internal UI state handling.
            filtered = mentionTools.filter(tool => {
                return tool.externalMcp && tool.externalMcp.toLowerCase() === exactMatchedMcp.toLowerCase();
            });
        } else {
            // Internal UI state handling.
            const partialMatchedMcps = externalMcpNames.filter(mcpName => 
                mcpName.toLowerCase().includes(normalizedQuery)
            );
            
            // Internal UI state handling.
            filtered = mentionTools.filter(tool => {
                const nameMatch = tool.name.toLowerCase().includes(normalizedQuery);
                const descMatch = tool.description && tool.description.toLowerCase().includes(normalizedQuery);
                const mcpMatch = tool.externalMcp && tool.externalMcp.toLowerCase().includes(normalizedQuery);
                
                // Internal UI state handling.
                const mcpPartialMatch = partialMatchedMcps.some(mcpName => 
                    tool.externalMcp && tool.externalMcp.toLowerCase() === mcpName.toLowerCase()
                );
                
                return nameMatch || descMatch || mcpMatch || mcpPartialMatch;
            });
        }
    }

    filtered = filtered.slice().sort((a, b) => {
        // Internal UI state handling.
        if (a.roleEnabled !== undefined || b.roleEnabled !== undefined) {
            const aRoleEnabled = a.roleEnabled !== undefined ? a.roleEnabled : a.enabled;
            const bRoleEnabled = b.roleEnabled !== undefined ? b.roleEnabled : b.enabled;
            if (aRoleEnabled !== bRoleEnabled) {
                return aRoleEnabled ? -1 : 1; // tools
            }
        }

        if (normalizedQuery) {
            // Internal UI state handling.
            const aMcpExact = a.externalMcp && a.externalMcp.toLowerCase() === normalizedQuery;
            const bMcpExact = b.externalMcp && b.externalMcp.toLowerCase() === normalizedQuery;
            if (aMcpExact !== bMcpExact) {
                return aMcpExact ? -1 : 1;
            }
            
            const aStarts = a.name.toLowerCase().startsWith(normalizedQuery);
            const bStarts = b.name.toLowerCase().startsWith(normalizedQuery);
            if (aStarts !== bStarts) {
                return aStarts ? -1 : 1;
            }
        }
        // Internal UI state handling.
        const aEnabled = a.roleEnabled !== undefined ? a.roleEnabled : a.enabled;
        const bEnabled = b.roleEnabled !== undefined ? b.roleEnabled : b.enabled;
        if (aEnabled !== bEnabled) {
            return aEnabled ? -1 : 1;
        }
        return a.name.localeCompare(b.name, 'zh-CN');
    });

    mentionFilteredTools = filtered;
    if (mentionFilteredTools.length === 0) {
        mentionState.selectedIndex = 0;
    } else if (mentionState.selectedIndex >= mentionFilteredTools.length) {
        mentionState.selectedIndex = 0;
    }
}

function renderMentionSuggestions({ showLoading = false } = {}) {
    if (!mentionSuggestionsEl || !mentionState.active) {
        hideMentionSuggestions();
        return;
    }

    const currentQuery = mentionState.query || '';
    const existingList = mentionSuggestionsEl.querySelector('.mention-suggestions-list');
    const canPreserveScroll = !showLoading &&
        existingList &&
        mentionSuggestionsEl.dataset.lastMentionQuery === currentQuery;
    const previousScrollTop = canPreserveScroll ? existingList.scrollTop : 0;

    if (showLoading) {
        mentionSuggestionsEl.innerHTML = '<div class="mention-empty">' + (typeof window.t === 'function' ? window.t('chat.loadingTools') : 'tools...') + '</div>';
        mentionSuggestionsEl.style.display = 'block';
        delete mentionSuggestionsEl.dataset.lastMentionQuery;
        return;
    }

    if (!mentionFilteredTools.length) {
        mentionSuggestionsEl.innerHTML = '<div class="mention-empty">' + (typeof window.t === 'function' ? window.t('chat.noMatchTools') : 'tools') + '</div>';
        mentionSuggestionsEl.style.display = 'block';
        mentionSuggestionsEl.dataset.lastMentionQuery = currentQuery;
        return;
    }

    const itemsHtml = mentionFilteredTools.map((tool, index) => {
        const activeClass = index === mentionState.selectedIndex ? 'active' : '';
        // Internal UI state handling.
        const toolEnabled = tool.roleEnabled !== undefined ? tool.roleEnabled : tool.enabled;
        const disabledClass = toolEnabled ? '' : 'disabled';
        const badge = tool.isExternal ? '<span class="mention-item-badge">External</span>' : '<span class="mention-item-badge internal">Built-in</span>';
        const nameHtml = escapeHtml(tool.name);
        const description = tool.description && tool.description.length > 0 ? escapeHtml(tool.description) : (typeof window.t === 'function' ? window.t('chat.noDescription') : 'No description');
        const descHtml = `<div class="mention-item-desc">${description}</div>`;
        // Internal UI state handling.
        const statusLabel = toolEnabled ? 'Enabled' : (tool.roleEnabled !== undefined ? 'Disabled by role' : 'Disabled');
        const statusClass = toolEnabled ? 'enabled' : 'disabled';
        const originLabel = tool.isExternal
            ? (tool.externalMcp ? `MCP: ${escapeHtml(tool.externalMcp)}` : 'Source: MCP')
            : 'Source: tools';

        return `
            <button type="button" class="mention-item ${activeClass} ${disabledClass}" data-index="${index}">
                <div class="mention-item-name">
                    <span class="mention-item-icon">🔧</span>
                    <span class="mention-item-text">@${nameHtml}</span>
                    ${badge}
                </div>
                ${descHtml}
                <div class="mention-item-meta">
                    <span class="mention-status ${statusClass}">${statusLabel}</span>
                    <span class="mention-origin">${originLabel}</span>
                </div>
            </button>
        `;
    }).join('');

    const listWrapper = document.createElement('div');
    listWrapper.className = 'mention-suggestions-list';
    listWrapper.innerHTML = itemsHtml;

    mentionSuggestionsEl.innerHTML = '';
    mentionSuggestionsEl.appendChild(listWrapper);
    mentionSuggestionsEl.style.display = 'block';
    mentionSuggestionsEl.dataset.lastMentionQuery = currentQuery;

    if (canPreserveScroll) {
        listWrapper.scrollTop = previousScrollTop;
    }

    listWrapper.querySelectorAll('.mention-item').forEach(item => {
        item.addEventListener('mousedown', (event) => {
            event.preventDefault();
            const idx = parseInt(item.dataset.index, 10);
            if (!Number.isNaN(idx)) {
                mentionState.selectedIndex = idx;
            }
            applyMentionSelection();
        });
    });

    scrollMentionSelectionIntoView();
}

function hideMentionSuggestions() {
    if (mentionSuggestionsEl) {
        mentionSuggestionsEl.style.display = 'none';
        mentionSuggestionsEl.innerHTML = '';
        delete mentionSuggestionsEl.dataset.lastMentionQuery;
    }
}

function deactivateMentionState() {
    mentionState.active = false;
    mentionState.startIndex = -1;
    mentionState.query = '';
    mentionState.selectedIndex = 0;
    mentionFilteredTools = [];
    hideMentionSuggestions();
}

function moveMentionSelection(direction) {
    if (!mentionFilteredTools.length) {
        return;
    }
    const max = mentionFilteredTools.length - 1;
    let nextIndex = mentionState.selectedIndex + direction;
    if (nextIndex < 0) {
        nextIndex = max;
    } else if (nextIndex > max) {
        nextIndex = 0;
    }
    mentionState.selectedIndex = nextIndex;
    updateMentionActiveHighlight();
}

function updateMentionActiveHighlight() {
    if (!mentionSuggestionsEl) {
        return;
    }
    const items = mentionSuggestionsEl.querySelectorAll('.mention-item');
    if (!items.length) {
        return;
    }
    items.forEach(item => item.classList.remove('active'));

    let targetIndex = mentionState.selectedIndex;
    if (targetIndex < 0) {
        targetIndex = 0;
    }
    if (targetIndex >= items.length) {
        targetIndex = items.length - 1;
        mentionState.selectedIndex = targetIndex;
    }

    const activeItem = items[targetIndex];
    if (activeItem) {
        activeItem.classList.add('active');
        scrollMentionSelectionIntoView(activeItem);
    }
}

function scrollMentionSelectionIntoView(targetItem = null) {
    if (!mentionSuggestionsEl) {
        return;
    }
    const activeItem = targetItem || mentionSuggestionsEl.querySelector('.mention-item.active');
    if (activeItem && typeof activeItem.scrollIntoView === 'function') {
        activeItem.scrollIntoView({
            block: 'nearest',
            inline: 'nearest',
            behavior: 'auto'
        });
    }
}

function applyMentionSelection() {
    const textarea = document.getElementById('chat-input');
    if (!textarea || mentionState.startIndex === -1 || !mentionFilteredTools.length) {
        deactivateMentionState();
        return;
    }

    const selectedTool = mentionFilteredTools[mentionState.selectedIndex] || mentionFilteredTools[0];
    if (!selectedTool) {
        deactivateMentionState();
        return;
    }

    const caret = textarea.selectionStart || 0;
    const before = textarea.value.slice(0, mentionState.startIndex);
    const after = textarea.value.slice(caret);
    const mentionText = `@${selectedTool.name}`;
    const needsSpace = after.length === 0 || !/^\s/.test(after);
    const insertText = mentionText + (needsSpace ? ' ' : '');

    textarea.value = before + insertText + after;
    const newCaret = before.length + insertText.length;
    textarea.focus();
    textarea.setSelectionRange(newCaret, newCaret);
    
    // Internal UI state handling.
    adjustTextareaHeight(textarea);
    saveChatDraftDebounced(textarea.value);

    deactivateMentionState();
}

function initializeChatUI() {
    const chatInputEl = document.getElementById('chat-input');
    if (chatInputEl) {
        // Internal UI state handling.
        adjustTextareaHeight(chatInputEl);
        // Internal UI state handling.
        if (!chatInputEl.value || chatInputEl.value.trim() === '') {
            // Internal UI state handling.
            const messagesDiv = document.getElementById('chat-messages');
            let shouldRestoreDraft = true;
            if (messagesDiv && messagesDiv.children.length > 0) {
                // Internal UI state handling.
                const lastMessage = messagesDiv.lastElementChild;
                if (lastMessage) {
                    const timeDiv = lastMessage.querySelector('.message-time');
                    if (timeDiv && timeDiv.textContent) {
                        // Internal UI state handling.
                        const isUserMessage = lastMessage.classList.contains('user');
                        if (isUserMessage) {
                            // Internal UI state handling.
                            const now = new Date();
                            const messageTimeText = timeDiv.textContent;
                            // Internal UI state handling.
                            // Internal UI state handling.
                            // Internal UI state handling.
                            shouldRestoreDraft = false;
                        }
                    }
                }
            }
            if (shouldRestoreDraft) {
                restoreChatDraft();
            } else {
                // Internal UI state handling.
                clearChatDraft();
            }
        }
    }

    const messagesDiv = document.getElementById('chat-messages');
    if (messagesDiv && messagesDiv.childElementCount === 0) {
        const readyMsg = typeof window.t === 'function' ? window.t('chat.systemReadyMessage') : 'System is ready. Automatic execution safety checks are enabled.';
        addMessage('assistant', readyMsg, null, null, null, { systemReadyMessage: true });
    }

    addAttackChainButton(currentConversationId);
    loadActiveTasks(true);
    if (activeTaskInterval) {
        clearInterval(activeTaskInterval);
    }
    activeTaskInterval = setInterval(() => loadActiveTasks(), ACTIVE_TASK_REFRESH_INTERVAL);
    setupMentionSupport();
    ensureChatInputContainerId();
    setupChatFileUpload();
}

// Internal UI state handling.
let messageCounter = 0;

// Internal UI state handling.
function wrapTablesInBubble(bubble) {
    const tables = bubble.querySelectorAll('table');
    tables.forEach(table => {
        // Internal UI state handling.
        if (table.parentElement && table.parentElement.classList.contains('table-wrapper')) {
            return;
        }
        
        // Internal UI state handling.
        const wrapper = document.createElement('div');
        wrapper.className = 'table-wrapper';
        
        // Internal UI state handling.
        table.parentNode.insertBefore(wrapper, table);
        wrapper.appendChild(table);
    });
}

/**
 * Internal UI state handling.
 */
function refreshSystemReadyMessageBubbles() {
    if (typeof window.t !== 'function') return;
    const text = window.t('chat.systemReadyMessage');
    const escapeHtmlLocal = (s) => {
        if (!s) return '';
        const div = document.createElement('div');
        div.textContent = s;
        return div.innerHTML;
    };
    const defaultSanitizeConfig = {
        ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'u', 's', 'code', 'pre', 'blockquote', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'li', 'a', 'img', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'hr'],
        ALLOWED_ATTR: ['href', 'title', 'alt', 'src', 'class'],
        ALLOW_DATA_ATTR: false,
    };
    let formattedContent;
    if (typeof marked !== 'undefined') {
        try {
            marked.setOptions({ breaks: true, gfm: true });
            const src = typeof window.normalizeAssistantMarkdownSource === 'function'
                ? window.normalizeAssistantMarkdownSource(text)
                : text;
            const parsed = marked.parse(src, { async: false });
            formattedContent = typeof DOMPurify !== 'undefined'
                ? DOMPurify.sanitize(parsed, defaultSanitizeConfig)
                : parsed;
        } catch (e) {
            formattedContent = escapeHtmlLocal(text).replace(/\n/g, '<br>');
        }
    } else {
        formattedContent = escapeHtmlLocal(text).replace(/\n/g, '<br>');
    }

    document.querySelectorAll('.message.assistant[data-system-ready-message]').forEach(function (messageDiv) {
        const bubble = messageDiv.querySelector('.message-bubble');
        if (!bubble) return;
        const copyBtn = bubble.querySelector('.message-copy-btn');
        if (copyBtn) copyBtn.remove();
        bubble.innerHTML = formattedContent;
        if (typeof wrapTablesInBubble === 'function') wrapTablesInBubble(bubble);
        messageDiv.dataset.originalContent = text;
        const copyBtnNew = document.createElement('button');
        copyBtnNew.className = 'message-copy-btn';
        copyBtnNew.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/></svg><span>' + window.t('common.copy') + '</span>';
        copyBtnNew.title = window.t('chat.copyMessageTitle');
        copyBtnNew.onclick = function (e) {
            e.stopPropagation();
            copyMessageToClipboard(messageDiv, this);
        };
        bubble.appendChild(copyBtnNew);
    });
}

// Internal UI state handling.
function addMessage(role, content, mcpExecutionIds = null, progressId = null, createdAt = null, options = null) {
    const messagesDiv = document.getElementById('chat-messages');
    const messageDiv = document.createElement('div');
    messageCounter++;
    const id = 'msg-' + Date.now() + '-' + messageCounter + '-' + Math.random().toString(36).substr(2, 9);
    messageDiv.id = id;
    messageDiv.className = 'message ' + role;
    
    // Internal UI state handling.
    const avatar = document.createElement('div');
    avatar.className = 'message-avatar';
    if (role === 'user') {
        avatar.textContent = 'U';
    } else if (role === 'assistant') {
        avatar.textContent = 'A';
    } else {
        avatar.textContent = 'S';
    }
    messageDiv.appendChild(avatar);
    
    // Internal UI state handling.
    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'message-content';
    
    // Internal UI state handling.
    const bubble = document.createElement('div');
    bubble.className = 'message-bubble';
    
    // Internal UI state handling.
    let formattedContent;
    const defaultSanitizeConfig = {
        ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'u', 's', 'code', 'pre', 'blockquote', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'li', 'a', 'img', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'hr'],
        ALLOWED_ATTR: ['href', 'title', 'alt', 'src', 'class'],
        ALLOW_DATA_ATTR: false,
    };
    
    // Internal UI state handling.
    const escapeHtml = (text) => {
        if (!text) return '';
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    };
    
    // Internal UI state handling.
    // Internal UI state handling.
    // Internal UI state handling.
    // Internal UI state handling.
    // Internal UI state handling.
    
    const parseMarkdown = (raw) => {
        if (typeof marked === 'undefined') {
            return null;
        }
        try {
            marked.setOptions({
                breaks: true,
                gfm: true,
            });
            const src = typeof window.normalizeAssistantMarkdownSource === 'function'
                ? window.normalizeAssistantMarkdownSource(raw)
                : raw;
            return marked.parse(src, { async: false });
        } catch (e) {
            console.error('Markdown Failed:', e);
            return null;
        }
    };
    
    // Internal UI state handling.
    let displayContent = content;
    if (role === 'assistant' && typeof displayContent === 'string' && typeof window.t === 'function') {
        if (displayContent.indexOf('failed: ') === 0) {
            displayContent = window.t('chat.executeFailed') + ': ' + displayContent.slice('failed: '.length);
        }
        if (displayContent.indexOf('OpenAI call failed:') !== -1) {
            displayContent = displayContent.replace(/OpenAI call failed:/g, window.t('chat.callOpenAIFailed') + ':');
        }
    }

    // Internal UI state handling.
    if (role === 'user') {
        formattedContent = escapeHtml(content).replace(/\n/g, '<br>');
    } else if (typeof DOMPurify !== 'undefined') {
        // Internal UI state handling.
        let parsedContent = parseMarkdown(role === 'assistant' ? displayContent : content);
        if (!parsedContent) {
            parsedContent = content;
        }
        
        // Internal UI state handling.
        if (DOMPurify.addHook) {
            // Internal UI state handling.
            try {
                DOMPurify.removeHook('uponSanitizeAttribute');
            } catch (e) {
                // Internal UI state handling.
            }
            
            // Internal UI state handling.
            DOMPurify.addHook('uponSanitizeAttribute', (node, data) => {
                const attrName = data.attrName.toLowerCase();
                
                // Internal UI state handling.
                if ((attrName === 'src' || attrName === 'href') && data.attrValue) {
                    const value = data.attrValue.trim().toLowerCase();
                    // Internal UI state handling.
                    if (value.startsWith('javascript:') || 
                        value.startsWith('vbscript:') ||
                        value.startsWith('data:text/html') ||
                        value.startsWith('data:text/javascript')) {
                        data.keepAttr = false;
                        return;
                    }
                    // Internal UI state handling.
                    if (attrName === 'src' && node.tagName && node.tagName.toLowerCase() === 'img') {
                        if (value.length <= 2 || /^[a-z]$/i.test(value)) {
                            data.keepAttr = false;
                            return;
                        }
                    }
                }
            });
        }
        
        formattedContent = DOMPurify.sanitize(parsedContent, defaultSanitizeConfig);
    } else if (typeof marked !== 'undefined') {
        const rawForParse = role === 'assistant' ? displayContent : content;
        const parsedContent = parseMarkdown(rawForParse);
        if (parsedContent) {
            formattedContent = parsedContent;
        } else {
            formattedContent = escapeHtml(rawForParse).replace(/\n/g, '<br>');
        }
    } else {
        const rawForEscape = role === 'assistant' ? displayContent : content;
        formattedContent = escapeHtml(rawForEscape).replace(/\n/g, '<br>');
    }
    
    bubble.innerHTML = formattedContent;
    
    // Internal UI state handling.
    // Internal UI state handling.
    const images = bubble.querySelectorAll('img');
    images.forEach(img => {
        const src = img.getAttribute('src');
        if (src) {
            const trimmedSrc = src.trim();
            // Internal UI state handling.
            if (trimmedSrc.length <= 2 || /^[a-z]$/i.test(trimmedSrc)) {
                img.remove();
            }
        } else {
            img.remove();
        }
    });
    
    // Internal UI state handling.
    wrapTablesInBubble(bubble);
    
    contentWrapper.appendChild(bubble);
    
    // Internal UI state handling.
    if (role === 'assistant') {
        messageDiv.dataset.originalContent = content;
    }
    
    // Add a copy button for assistant messages, positioned at the lower-right of the bubble.
    if (role === 'assistant') {
        const copyBtn = document.createElement('button');
        copyBtn.className = 'message-copy-btn';
        copyBtn.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/></svg><span>' + (typeof window.t === 'function' ? window.t('common.copy') : 'Copy') + '</span>';
        copyBtn.title = typeof window.t === 'function' ? window.t('chat.copyMessageTitle') : 'Copy message content';
        copyBtn.onclick = function(e) {
            e.stopPropagation();
            copyMessageToClipboard(messageDiv, this);
        };
        bubble.appendChild(copyBtn);
    }
    
    // Internal UI state handling.
    const timeDiv = document.createElement('div');
    timeDiv.className = 'message-time';
    // Internal UI state handling.
    let messageTime;
    if (createdAt) {
        // Internal UI state handling.
        if (typeof createdAt === 'string') {
            messageTime = new Date(createdAt);
        } else if (createdAt instanceof Date) {
            messageTime = createdAt;
        } else {
            messageTime = new Date(createdAt);
        }
        // Internal UI state handling.
        if (isNaN(messageTime.getTime())) {
            messageTime = new Date();
        }
    } else {
        messageTime = new Date();
    }
    const msgTimeLocale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
    const msgTimeOpts = { hour: '2-digit', minute: '2-digit' };
    if (msgTimeLocale === 'zh-CN') msgTimeOpts.hour12 = false;
    timeDiv.textContent = messageTime.toLocaleTimeString(msgTimeLocale, msgTimeOpts);
    try {
        timeDiv.dataset.messageTime = messageTime.toISOString();
    } catch (e) { /* ignore */ }
    contentWrapper.appendChild(timeDiv);
    
    // Internal UI state handling.
    if (role === 'assistant' && (mcpExecutionIds && Array.isArray(mcpExecutionIds) && mcpExecutionIds.length > 0) && !progressId) {
        const mcpSection = document.createElement('div');
        mcpSection.className = 'mcp-call-section';
        
        const mcpLabel = document.createElement('div');
        mcpLabel.className = 'mcp-call-label';
        mcpLabel.textContent = '📋 ' + (typeof window.t === 'function' ? window.t('chat.penetrationTestDetail') : 'Penetration test details');
        mcpSection.appendChild(mcpLabel);
        
        const buttonsContainer = document.createElement('div');
        buttonsContainer.className = 'mcp-call-buttons';
        
        mcpExecutionIds.forEach((execId, index) => {
            const detailBtn = document.createElement('button');
            detailBtn.className = 'mcp-detail-btn';
            detailBtn.dataset.execId = execId;
            detailBtn.dataset.execIndex = String(index + 1);
            detailBtn.innerHTML = '<span>' + (typeof window.t === 'function' ? window.t('chat.callNumber', { n: index + 1 }) : 'Call #' + (index + 1)) + '</span>';
            detailBtn.onclick = () => showMCPDetail(execId);
            buttonsContainer.appendChild(detailBtn);
        });
        // Internal UI state handling.
        batchUpdateButtonToolNames(buttonsContainer, mcpExecutionIds);
        
        mcpSection.appendChild(buttonsContainer);
        contentWrapper.appendChild(mcpSection);
    }
    
    messageDiv.appendChild(contentWrapper);
    // Internal UI state handling.
    if (options && options.systemReadyMessage) {
        messageDiv.setAttribute('data-system-ready-message', '1');
    }
    messagesDiv.appendChild(messageDiv);
    if (window.CyberStrikeChatScroll) {
        window.CyberStrikeChatScroll.applyMessageScroll(options);
    } else {
        messagesDiv.scrollTop = messagesDiv.scrollHeight;
    }
    return id;
}

// Internal UI state handling.
function copyMessageToClipboard(messageDiv, button) {
    try {
        // Internal UI state handling.
        const originalContent = messageDiv.dataset.originalContent;

        // Internal UI state handling.
        const doCopy = (text) => {
            // Internal UI state handling.
            if (navigator.clipboard && navigator.clipboard.writeText) {
                return navigator.clipboard.writeText(text).then(() => {
                    showCopySuccess(button);
                }).catch(err => {
                    console.error('Clipboard API Failed:', err);
                    fallbackCopy(text);
                });
            } else {
                // Internal UI state handling.
                return fallbackCopy(text);
            }
        };

        // Internal UI state handling.
        const fallbackCopy = (text) => {
            try {
                const textArea = document.createElement('textarea');
                textArea.value = text;
                textArea.style.position = 'fixed';
                textArea.style.left = '-999999px';
                textArea.style.top = '-999999px';
                textArea.style.opacity = '0';
                document.body.appendChild(textArea);
                textArea.focus();
                textArea.select();

                const successful = document.execCommand('copy');
                document.body.removeChild(textArea);

                if (successful) {
                    showCopySuccess(button);
                } else {
                    throw new Error('execCommand copy failed');
                }
            } catch (execErr) {
                console.error('Failed:', execErr);
                alert(typeof window.t === 'function' ? window.t('chat.copyFailedManual') : 'Copy failed; please select and copy manually.');
            }
        };

        if (!originalContent) {
            // Internal UI state handling.
            const bubble = messageDiv.querySelector('.message-bubble');
            if (bubble) {
                const tempDiv = document.createElement('div');
                tempDiv.innerHTML = bubble.innerHTML;
                
                // Internal UI state handling.
                const copyBtnInTemp = tempDiv.querySelector('.message-copy-btn');
                if (copyBtnInTemp) {
                    copyBtnInTemp.remove();
                }
                
                // Internal UI state handling.
                let textContent = tempDiv.textContent || tempDiv.innerText || '';
                textContent = textContent.replace(/\n{3,}/g, '\n\n').trim();

                doCopy(textContent);
            }
            return;
        }
        
        // Internal UI state handling.
        doCopy(originalContent);
    } catch (error) {
        console.error('Error:', error);
        alert(typeof window.t === 'function' ? window.t('chat.copyFailedManual') : 'Copy failed; please select and copy manually.');
    }
}

// Show copy-success feedback.
function showCopySuccess(button) {
    if (button) {
        const originalText = button.innerHTML;
        button.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M20 6L9 17l-5-5" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/></svg><span>' + (typeof window.t === 'function' ? window.t('common.copied') : 'Copied') + '</span>';
        button.style.color = '#10b981';
        button.style.background = 'rgba(16, 185, 129, 0.1)';
        button.style.borderColor = 'rgba(16, 185, 129, 0.3)';
        setTimeout(() => {
            button.innerHTML = originalText;
            button.style.color = '';
            button.style.background = '';
            button.style.borderColor = '';
        }, 2000);
    }
}

/** Internal UI state handling. */
function dedupeConsecutiveProcessDetailRows(details) {
    if (!Array.isArray(details) || details.length < 2) {
        return details;
    }
    const out = [details[0]];
    for (let i = 1; i < details.length; i++) {
        const cur = details[i];
        if (processDetailRowFingerprint(out[out.length - 1]) === processDetailRowFingerprint(cur)) {
            continue;
        }
        out.push(cur);
    }
    return out;
}

function processDetailRowFingerprint(d) {
    if (!d || typeof d !== 'object') {
        return '';
    }
    const et = String(d.eventType || '');
    const msg = String(d.message != null ? d.message : '').trim();
    let dataKey = '';
    try {
        if (d.data != null) {
            dataKey = JSON.stringify(d.data);
        }
    } catch (e) {
        dataKey = String(d.data);
    }
    return et + '\0' + msg + '\0' + dataKey;
}

// Internal UI state handling.
function renderProcessDetails(messageId, processDetails) {
    const messageElement = document.getElementById(messageId);
    if (!messageElement) {
        return;
    }
    
    // Internal UI state handling.
    let mcpSection = messageElement.querySelector('.mcp-call-section');
    if (!mcpSection) {
        mcpSection = document.createElement('div');
        mcpSection.className = 'mcp-call-section';
        
        const contentWrapper = messageElement.querySelector('.message-content');
        if (contentWrapper) {
            contentWrapper.appendChild(mcpSection);
        } else {
            return;
        }
    }
    
    // Internal UI state handling.
    let mcpLabel = mcpSection.querySelector('.mcp-call-label');
    let buttonsContainer = mcpSection.querySelector('.mcp-call-buttons');
    
    // Internal UI state handling.
    if (!mcpLabel && !buttonsContainer) {
        mcpLabel = document.createElement('div');
        mcpLabel.className = 'mcp-call-label';
        mcpLabel.textContent = '📋 ' + (typeof window.t === 'function' ? window.t('chat.penetrationTestDetail') : 'Penetration test details');
        mcpSection.appendChild(mcpLabel);
    } else if (mcpLabel && mcpLabel.textContent !== ('📋 ' + (typeof window.t === 'function' ? window.t('chat.penetrationTestDetail') : 'Penetration test details'))) {
        // Internal UI state handling.
        mcpLabel.textContent = '📋 ' + (typeof window.t === 'function' ? window.t('chat.penetrationTestDetail') : 'Penetration test details');
    }
    
    // Internal UI state handling.
    if (!buttonsContainer) {
        buttonsContainer = document.createElement('div');
        buttonsContainer.className = 'mcp-call-buttons';
        mcpSection.appendChild(buttonsContainer);
    }
    
    // Internal UI state handling.
    let processDetailBtn = buttonsContainer.querySelector('.process-detail-btn');
    if (!processDetailBtn) {
        processDetailBtn = document.createElement('button');
        processDetailBtn.className = 'mcp-detail-btn process-detail-btn';
        processDetailBtn.innerHTML = '<span>' + (typeof window.t === 'function' ? window.t('chat.expandDetail') : 'Expand details') + '</span>';
        processDetailBtn.onclick = () => toggleProcessDetails(null, messageId);
        buttonsContainer.appendChild(processDetailBtn);
    }
    
    // Internal UI state handling.
    const detailsId = 'process-details-' + messageId;
    let detailsContainer = document.getElementById(detailsId);
    
    if (!detailsContainer) {
        detailsContainer = document.createElement('div');
        detailsContainer.id = detailsId;
        detailsContainer.className = 'process-details-container';
        // Internal UI state handling.
        if (buttonsContainer.nextSibling) {
            mcpSection.insertBefore(detailsContainer, buttonsContainer.nextSibling);
        } else {
            mcpSection.appendChild(detailsContainer);
        }
    }
    
    // Internal UI state handling.
    const timelineId = detailsId + '-timeline';
    let timeline = document.getElementById(timelineId);
    
    if (!timeline) {
        const contentDiv = document.createElement('div');
        contentDiv.className = 'process-details-content';
        
        timeline = document.createElement('div');
        timeline.id = timelineId;
        timeline.className = 'progress-timeline';
        
        contentDiv.appendChild(timeline);
        detailsContainer.appendChild(contentDiv);
    }
    
    // Internal UI state handling.
    const isLazyNotLoaded = (processDetails === null);
    if (isLazyNotLoaded) {
        detailsContainer.dataset.lazyNotLoaded = '1';
        detailsContainer.dataset.loaded = '0';
        timeline.innerHTML = '<div class="progress-timeline-empty">' +
            (typeof window.t === 'function' ? window.t('chat.expandDetail') : 'Expand details') +
            ' (click to load)</div>';
        // Internal UI state handling.
        timeline.classList.remove('expanded');
        return;
    }
    detailsContainer.dataset.lazyNotLoaded = '0';
    detailsContainer.dataset.loaded = '1';
    processDetails = dedupeConsecutiveProcessDetailRows(processDetails);
    if (typeof window.coalesceProcessDetailsToolPairs === 'function') {
        processDetails = window.coalesceProcessDetailsToolPairs(processDetails);
    }
    // Internal UI state handling.
    if (!processDetails || processDetails.length === 0) {
        // Internal UI state handling.
        timeline.innerHTML = '<div class="progress-timeline-empty">' + (typeof window.t === 'function' ? window.t('chat.noProcessDetail') : 'No process details (the run may have finished quickly or produced no detail events)') + '</div>';
        // Internal UI state handling.
        timeline.classList.remove('expanded');
        return;
    }
    
    // Internal UI state handling.
    timeline.innerHTML = '';
    
    
    function processDetailAgentPrefix(d) {
        if (!d || d.einoAgent == null) return '';
        const s = String(d.einoAgent).trim();
        return s ? ('[' + s + '] ') : '';
    }

    // Internal UI state handling.
    processDetails.forEach(detail => {
        const eventType = detail.eventType || '';
        const title = detail.message || '';
        const data = detail.data || {};
        const agPx = processDetailAgentPrefix(data);
        
        // Internal UI state handling.
        let itemTitle = title;
        if (eventType === 'iteration') {
            const n = data.iteration || 1;
            if (data.orchestration === 'plan_execute' && data.einoScope === 'main') {
                const phase = typeof window.translatePlanExecuteAgentName === 'function'
                    ? window.translatePlanExecuteAgentName(data.einoAgent) : (data.einoAgent || '');
                itemTitle = (typeof window.t === 'function'
                    ? window.t('chat.einoPlanExecuteRound', { n: n, phase: phase })
                    : ('Plan-Execute · Round ' + n + '  · ' + phase));
            } else if (data.einoScope === 'main') {
                itemTitle = agPx + (typeof window.t === 'function'
                    ? window.t('chat.einoOrchestratorRound', { n: n })
                    : ('Main agent · Round ' + n + ' iteration'));
            } else if (data.einoScope === 'sub') {
                const agent = data.einoAgent != null ? String(data.einoAgent).trim() : '';
                itemTitle = agPx + (typeof window.t === 'function'
                    ? window.t('chat.einoSubAgentStep', { n: n, agent: agent })
                    : ('Sub-agent · ' + agent + ' · Round ' + n + ' step'));
            } else {
                itemTitle = agPx + (typeof window.t === 'function' ? window.t('chat.iterationRound', { n: n }) : 'Round ' + n + ' iterations');
            }
        } else if (eventType === 'thinking') {
            itemTitle = agPx + '🤔 ' + (typeof window.t === 'function' ? window.t('chat.aiThinking') : 'AIThought');
        } else if (eventType === 'reasoning_chain') {
            itemTitle = agPx + '🔗 ' + (typeof window.t === 'function' ? window.t('chat.reasoningChain') : 'Reasoning process');
        } else if (eventType === 'planning') {
            if (typeof window.einoMainStreamPlanningTitle === 'function') {
                itemTitle = window.einoMainStreamPlanningTitle(data);
            } else {
                itemTitle = agPx + '📝 ' + (typeof window.t === 'function' ? window.t('chat.planning') : 'Planning');
            }
        } else if (eventType === 'tool_calls_detected') {
            itemTitle = agPx + '🔧 ' + (typeof window.t === 'function' ? window.t('chat.toolCallsDetected', { count: data.count || 0 }) : 'Detected ' + (data.count || 0) + '  tool calls');
        } else if (eventType === 'tool_call') {
            const toolName = data.toolName || (typeof window.t === 'function' ? window.t('chat.unknownTool') : 'Unknown tool');
            const index = data.index || 0;
            const total = data.total || 0;
            const callTitle = typeof window.formatToolCallTimelineTitle === 'function'
                ? window.formatToolCallTimelineTitle(toolName, index, total)
                : (typeof window.t === 'function' ? window.t('chat.callTool', { name: escapeHtml(toolName), index: index, total: total }) : 'tools: ' + escapeHtml(toolName) + ' (' + index + '/' + total + ')');
            itemTitle = agPx + '🔧 ' + callTitle;
        } else if (eventType === 'tool_result') {
            const toolName = data.toolName || (typeof window.t === 'function' ? window.t('chat.unknownTool') : 'Unknown tool');
            const success = data.success !== false;
            const statusIcon = success ? '✅' : '❌';
            const execText = success ? (typeof window.t === 'function' ? window.t('chat.toolExecComplete', { name: escapeHtml(toolName) }) : 'Tool ' + escapeHtml(toolName) + ' completed') : (typeof window.t === 'function' ? window.t('chat.toolExecFailed', { name: escapeHtml(toolName) }) : 'Tool ' + escapeHtml(toolName) + ' failed');
            let execLine = statusIcon + ' ' + execText;
            if (toolName === BuiltinTools.SEARCH_KNOWLEDGE_BASE && success) {
                execLine = '📚 ' + execLine + ' - ' + (typeof window.t === 'function' ? window.t('chat.knowledgeRetrievalTag') : 'Knowledge');
            }
            itemTitle = agPx + execLine;
        } else if (eventType === 'eino_agent_reply') {
            itemTitle = agPx + '💬 ' + (typeof window.t === 'function' ? window.t('chat.einoAgentReplyTitle') : 'Sub-agent response');
        } else if (eventType === 'eino_run_retry') {
            itemTitle = typeof window.t === 'function'
                ? window.t('chat.einoRunRetryTitle')
                : '🔁 Temporary error retry';
            const errRaw = data && data.error != null ? String(data.error).trim() : '';
            if (errRaw) {
                const detailLabel = typeof window.t === 'function'
                    ? window.t('chat.einoRunRetryErrorDetail')
                    : 'Error details';
                if (!title || String(title).indexOf(errRaw) === -1) {
                    const merged = title ? (String(title) + '\n' + detailLabel + ':' + errRaw) : (detailLabel + ':' + errRaw);
                    detail.message = merged;
                }
            }
        } else if (eventType === 'knowledge_retrieval') {
            itemTitle = '📚 ' + (typeof window.t === 'function' ? window.t('chat.knowledgeRetrieval') : 'Knowledge');
        } else if (eventType === 'error') {
            itemTitle = '❌ ' + (typeof window.t === 'function' ? window.t('chat.error') : 'Error');
        } else if (eventType === 'cancelled') {
            itemTitle = '⛔ ' + (typeof window.t === 'function' ? window.t('chat.taskCancelled') : 'Task cancelled');
        } else if (eventType === 'hitl_interrupt') {
            const hitlMsg = (detail.message && String(detail.message).trim()) ? String(detail.message).trim() : (typeof window.t === 'function' ? window.t('hitl.pendingTitle') : 'Copied');
            itemTitle = agPx + '🧑‍⚖️ HITL · ' + hitlMsg;
        } else if (eventType === 'progress') {
            itemTitle = typeof window.translateProgressMessage === 'function' ? window.translateProgressMessage(detail.message || '') : (detail.message || '');
        } else if (eventType === 'user_interrupt_continue') {
            itemTitle = typeof window.t === 'function'
                ? window.t('chat.userInterruptContinueTitle')
                : '⏸️ User interrupted and continued';
        }
        
        const timelineOpts = {
            title: itemTitle,
            message: detail.message || '',
            data: data,
            createdAt: detail.createdAt // event
        };
        if (eventType === 'tool_call' && data._mergedResult) {
            timelineOpts.mergedResult = data._mergedResult;
        }
        addTimelineItem(timeline, eventType, timelineOpts);
    });
    
    // Internal UI state handling.
    const hasPendingHitlInDetails = processDetails.some(d => d && d.eventType === 'hitl_interrupt');
    const hasErrorOrCancelled = processDetails.some(d => 
        d.eventType === 'error' || d.eventType === 'cancelled'
    );
    if (hasErrorOrCancelled && !hasPendingHitlInDetails) {
        // Internal UI state handling.
        timeline.classList.remove('expanded');
        // Internal UI state handling.
        const processDetailBtn = messageElement.querySelector('.process-detail-btn');
        if (processDetailBtn) {
            processDetailBtn.innerHTML = '<span>' + (typeof window.t === 'function' ? window.t('chat.expandDetail') : 'Expand details') + '</span>';
        }
    }
}

// Internal UI state handling.
function removeMessage(id) {
    const messageDiv = document.getElementById(id);
    if (messageDiv) {
        messageDiv.remove();
    }
}

// Internal UI state handling.
const chatInput = document.getElementById('chat-input');
if (chatInput) {
    chatInput.addEventListener('keydown', handleChatInputKeydown);
    chatInput.addEventListener('input', handleChatInputInput);
    chatInput.addEventListener('click', handleChatInputClick);
    chatInput.addEventListener('focus', handleChatInputClick);
    // Internal UI state handling.
    chatInput.addEventListener('compositionstart', () => {
        isComposing = true;
    });
    chatInput.addEventListener('compositionend', () => {
        isComposing = false;
    });
    chatInput.addEventListener('blur', () => {
        setTimeout(() => {
            if (!chatInput.matches(':focus')) {
                deactivateMentionState();
            }
        }, 120);
        // Internal UI state handling.
        if (chatInput.value) {
            saveChatDraft(chatInput.value);
        }
    });
}

// Internal UI state handling.
window.addEventListener('beforeunload', () => {
    const chatInput = document.getElementById('chat-input');
    if (chatInput && chatInput.value) {
        // Internal UI state handling.
        saveChatDraft(chatInput.value);
    }
});

// Internal UI state handling.
async function updateButtonWithToolName(button, executionId, index) {
    try {
        const response = await apiFetch(`/api/monitor/execution/${executionId}`);
        if (response.ok) {
            const exec = await response.json();
            const toolName = exec.toolName || (typeof window.t === 'function' ? window.t('chat.unknownTool') : 'Unknown tool');
            // Internal UI state handling.
            const displayToolName = toolName.includes('::') ? toolName.split('::')[1] : toolName;
            button.querySelector('span').textContent = `${displayToolName} #${index}`;
        }
    } catch (error) {
        // Internal UI state handling.
        console.error('toolsFailed:', error);
    }
}

// Internal UI state handling.
async function batchUpdateButtonToolNames(buttonsContainer, executionIds) {
    if (!executionIds || executionIds.length === 0) return;
    try {
        const response = await apiFetch('/api/monitor/executions/names', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ids: executionIds }),
        });
        if (!response.ok) return;
        const nameMap = await response.json(); // { execId: toolName }
        // Internal UI state handling.
        const buttons = buttonsContainer.querySelectorAll('.mcp-detail-btn[data-exec-id]');
        buttons.forEach(btn => {
            const execId = btn.dataset.execId;
            const index = btn.dataset.execIndex;
            const toolName = nameMap[execId];
            if (toolName) {
                const displayToolName = toolName.includes('::') ? toolName.split('::')[1] : toolName;
                const span = btn.querySelector('span');
                if (span) span.textContent = `${displayToolName} #${index}`;
            }
        });
    } catch (error) {
        console.error('toolsFailed:', error);
    }
}

// Internal UI state handling.
async function showMCPDetail(executionId) {
    try {
        const response = await apiFetch(`/api/monitor/execution/${executionId}`);
        const exec = await response.json();
        
        if (response.ok) {
            // Internal UI state handling.
            document.getElementById('detail-tool-name').textContent = exec.toolName || (typeof window.t === 'function' ? window.t('mcpDetailModal.unknown') : 'Unknown');
            document.getElementById('detail-execution-id').textContent = exec.id || 'N/A';
            const statusEl = document.getElementById('detail-status');
            const normalizedStatus = (exec.status || 'unknown').toLowerCase();
            statusEl.textContent = getStatusText(exec.status);
            statusEl.className = `status-chip status-${normalizedStatus}`;
            try {
                statusEl.dataset.detailStatus = (exec.status || '') + '';
            } catch (e) { /* ignore */ }
            const detailTimeLocale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
            const detailTimeEl = document.getElementById('detail-time');
            if (detailTimeEl) {
                detailTimeEl.textContent = exec.startTime
                    ? new Date(exec.startTime).toLocaleString(detailTimeLocale)
                    : '—';
                try {
                    detailTimeEl.dataset.detailTimeIso = exec.startTime ? new Date(exec.startTime).toISOString() : '';
                } catch (e) { /* ignore */ }
            }
            
            // Internal UI state handling.
            const requestData = {
                tool: exec.toolName,
                arguments: exec.arguments
            };
            document.getElementById('detail-request').textContent = JSON.stringify(requestData, null, 2);
            
            // Internal UI state handling.
            const responseElement = document.getElementById('detail-response');
            const successSection = document.getElementById('detail-success-section');
            const successElement = document.getElementById('detail-success');
            const errorSection = document.getElementById('detail-error-section');
            const errorElement = document.getElementById('detail-error');

            // ResetStatus
            responseElement.className = 'code-block';
            responseElement.textContent = '';
            if (successSection && successElement) {
                successSection.style.display = 'none';
                successElement.textContent = '';
            }
            if (errorSection && errorElement) {
                errorSection.style.display = 'none';
                errorElement.textContent = '';
            }

            if (exec.result) {
                const responseData = {
                    content: exec.result.content,
                    isError: exec.result.isError
                };
                responseElement.textContent = JSON.stringify(responseData, null, 2);

                if (exec.result.isError) {
                    // Internal UI state handling.
                    responseElement.className = 'code-block error';
                    if (exec.error && errorSection && errorElement) {
                        errorSection.style.display = 'block';
                        errorElement.textContent = exec.error;
                    }
                } else {
                    // Internal UI state handling.
                    responseElement.className = 'code-block';
                    if (successSection && successElement) {
                        successSection.style.display = 'block';
                        let successText = '';
                        const content = exec.result.content;
                        if (typeof content === 'string') {
                            successText = content;
                        } else if (Array.isArray(content)) {
                            const texts = content
                                .map(item => (item && typeof item === 'object' && typeof item.text === 'string') ? item.text : '')
                                .filter(Boolean);
                            if (texts.length > 0) {
                                successText = texts.join('\n\n');
                            }
                        } else if (content && typeof content === 'object' && typeof content.text === 'string') {
                            successText = content.text;
                        }
                        if (!successText) {
                            successText = typeof window.t === 'function' ? window.t('mcpDetailModal.execSuccessNoContent') : 'execution, text.';
                        }
                        successElement.textContent = successText;
                    }
                }
            } else {
                if (normalizedStatus === 'running') {
                    responseElement.textContent = typeof window.t === 'function' ? window.t('mcpDetailModal.runningNoResponseYet') : 'Still running. The tool will show its response when execution completes; you can terminate it if needed.textcalls.';
                } else {
                    responseElement.textContent = typeof window.t === 'function' ? window.t('chat.noResponseData') : 'No response data';
                }
            }

            const abortSection = document.getElementById('detail-abort-section');
            const abortBtn = document.getElementById('detail-abort-btn');
            if (abortSection && abortBtn) {
                if (normalizedStatus === 'running') {
                    abortSection.style.display = 'block';
                    abortBtn.dataset.execId = exec.id || '';
                    abortBtn.textContent = typeof window.t === 'function' ? window.t('mcpDetailModal.abortBtn') : 'Terminate tool';
                } else {
                    abortSection.style.display = 'none';
                    delete abortBtn.dataset.execId;
                }
            }
            
            // Internal UI state handling.
            document.getElementById('mcp-detail-modal').style.display = 'block';
        } else {
            alert((typeof window.t === 'function' ? window.t('mcpDetailModal.getDetailFailed') : 'Failed') + ': ' + (exec.error || (typeof window.t === 'function' ? window.t('mcpDetailModal.unknown') : 'UnknownError')));
        }
    } catch (error) {
        alert((typeof window.t === 'function' ? window.t('mcpDetailModal.getDetailFailed') : 'Failed') + ': ' + error.message);
    }
}

// Internal UI state handling.
function closeMCPDetail() {
    document.getElementById('mcp-detail-modal').style.display = 'none';
}

/** Internal UI state handling. */
async function abortMCPToolExecutionFromDetail() {
    const btn = document.getElementById('detail-abort-btn');
    const id = btn && btn.dataset.execId;
    if (!id) {
        return;
    }
    await cancelMCPToolExecution(id, { refreshDetail: true });
}

/**
 * Internal UI state handling.
 * @param {string} executionId
 * @param {{ refreshDetail?: boolean }} [options]
 */
function openMcpToolAbortModal(executionId, options = {}) {
    window.__mcpToolAbortContext = { executionId: executionId, options: options || {} };
    const ta = document.getElementById('mcp-tool-abort-note');
    if (ta) {
        ta.value = '';
    }
    const m = document.getElementById('mcp-tool-abort-modal');
    if (m) {
        m.style.display = 'block';
    }
}

function closeMcpToolAbortModal() {
    window.__mcpToolAbortContext = null;
    const m = document.getElementById('mcp-tool-abort-modal');
    if (m) {
        m.style.display = 'none';
    }
}

async function submitMcpToolAbortModal() {
    const ctx = window.__mcpToolAbortContext;
    if (!ctx || !ctx.executionId) {
        closeMcpToolAbortModal();
        return;
    }
    const note = (document.getElementById('mcp-tool-abort-note') && document.getElementById('mcp-tool-abort-note').value || '').trim();
    const executionId = ctx.executionId;
    const options = ctx.options || {};
    closeMcpToolAbortModal();
    await cancelMCPToolExecutionSubmit(executionId, note, options);
}

/**
 * Internal UI state handling.
 * @param {string} executionId
 * @param {string} userNote
 * @param {{ refreshDetail?: boolean }} [options]
 */
async function cancelMCPToolExecutionSubmit(executionId, userNote, options = {}) {
    if (!executionId) {
        return;
    }
    try {
        const res = await apiFetch(`/api/monitor/execution/${encodeURIComponent(executionId)}/cancel`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ note: userNote || '' }),
        });
        const body = await res.json().catch(() => ({}));
        if (!res.ok) {
            throw new Error(body.error || body.message || res.statusText);
        }
        const okMsg = typeof window.t === 'function' ? window.t('mcpDetailModal.abortSuccess') : 'Termination request sent';
        alert(okMsg);
        if (options.refreshDetail && typeof showMCPDetail === 'function') {
            await showMCPDetail(executionId);
        }
        if (typeof refreshMonitorPanel === 'function') {
            const page = (typeof monitorState !== 'undefined' && monitorState.pagination && monitorState.pagination.page) ? monitorState.pagination.page : 1;
            await refreshMonitorPanel(page);
        }
    } catch (e) {
        const failMsg = typeof window.t === 'function' ? window.t('mcpDetailModal.abortFailed') : 'TerminateFailed';
        alert(failMsg + ': ' + (e && e.message ? e.message : String(e)));
    }
}

/**
 * Internal UI state handling.
 * @param {string} executionId
 * @param {{ refreshDetail?: boolean }} [options]
 */
async function cancelMCPToolExecution(executionId, options = {}) {
    if (!executionId) {
        return;
    }
    openMcpToolAbortModal(executionId, options);
}

// Internal UI state handling.
function copyDetailBlock(elementId, triggerBtn = null) {
    const target = document.getElementById(elementId);
    if (!target) {
        return;
    }
    const text = target.textContent || '';
    if (!text.trim()) {
        return;
    }

    const originalLabel = triggerBtn ? (triggerBtn.dataset.originalLabel || triggerBtn.textContent.trim()) : '';
    if (triggerBtn && !triggerBtn.dataset.originalLabel) {
        triggerBtn.dataset.originalLabel = originalLabel;
    }

    const showCopiedState = () => {
        if (!triggerBtn) {
            return;
        }
        triggerBtn.textContent = 'Copied';
        triggerBtn.disabled = true;
        setTimeout(() => {
            triggerBtn.disabled = false;
            triggerBtn.textContent = triggerBtn.dataset.originalLabel || originalLabel || 'Copy';
        }, 1200);
    };

    const fallbackCopy = (value) => {
        return new Promise((resolve, reject) => {
            const textarea = document.createElement('textarea');
            textarea.value = value;
            textarea.style.position = 'fixed';
            textarea.style.opacity = '0';
            document.body.appendChild(textarea);
            textarea.focus();
            textarea.select();
            try {
                const successful = document.execCommand('copy');
                document.body.removeChild(textarea);
                if (successful) {
                    resolve();
                } else {
                    reject(new Error('execCommand failed'));
                }
            } catch (err) {
                document.body.removeChild(textarea);
                reject(err);
            }
        });
    };

    const copyPromise = (navigator.clipboard && typeof navigator.clipboard.writeText === 'function')
        ? navigator.clipboard.writeText(text)
        : fallbackCopy(text);

    copyPromise
        .then(() => {
            showCopiedState();
        })
        .catch(() => {
            if (triggerBtn) {
                triggerBtn.disabled = false;
                triggerBtn.textContent = triggerBtn.dataset.originalLabel || originalLabel || 'Copy';
            }
            alert('Copy failed; please select and copy manually.');
        });
}


// Internal UI state handling.
async function startNewConversation() {
    // Internal UI state handling.
    if (currentGroupId) {
        const groupDetailPage = document.getElementById('group-detail-page');
        const chatContainer = document.querySelector('.chat-container');
        if (groupDetailPage) groupDetailPage.style.display = 'none';
        if (chatContainer) chatContainer.style.display = 'flex';
        currentGroupId = null;
        // Internal UI state handling.
        loadConversationsWithGroups();
    }
    
    currentConversationId = null;
    window._loadedConversationProjectId = '';
    try {
        window.currentConversationId = '';
    } catch (e) { /* ignore */ }
    currentConversationGroupId = null; // New chatgroup
    if (typeof ensureDefaultActiveProjectForNewChat === 'function') {
        ensureDefaultActiveProjectForNewChat().catch(() => {});
    }
    if (typeof refreshChatProjectSelector === 'function') {
        refreshChatProjectSelector();
    }
    document.getElementById('chat-messages').innerHTML = '';
    const readyMsgNew = typeof window.t === 'function' ? window.t('chat.systemReadyMessage') : 'System is ready. Automatic execution safety checks are enabled.';
    addMessage('assistant', readyMsgNew, null, null, null, { systemReadyMessage: true });
    addAttackChainButton(null);
    updateActiveConversation();
    // Internal UI state handling.
    await loadGroups();
    // Internal UI state handling.
    loadConversationsWithGroups();
    // Internal UI state handling.
    if (draftSaveTimer) {
        clearTimeout(draftSaveTimer);
        draftSaveTimer = null;
    }
    // Internal UI state handling.
    clearChatDraft();
    // Internal UI state handling.
    const chatInput = document.getElementById('chat-input');
    if (chatInput) {
        chatInput.value = '';
        adjustTextareaHeight(chatInput);
    }
    // Internal UI state handling.
    try {
        if (typeof readHitlConfigFromForm === 'function' && typeof saveHitlConfigForConversation === 'function') {
            const snap = readHitlConfigFromForm();
            saveHitlConfigForConversation('', snap, { syncGlobalLast: true });
        }
    } catch (e) { /* ignore */ }
    refreshHitlConfigByCurrentConversation();
}

// Internal UI state handling.
async function loadConversations(searchQuery = '') {
    return loadConversationsWithGroups(searchQuery);
}

function createConversationListItem(conversation) {
    const item = document.createElement('div');
    item.className = 'conversation-item';
    item.dataset.conversationId = conversation.id;
    if (conversation.id === currentConversationId) {
        item.classList.add('active');
    }

    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'conversation-content';

    const title = document.createElement('div');
    title.className = 'conversation-title';
    const titleText = conversation.title || 'Chat';
    title.textContent = safeTruncateText(titleText, 60);
    title.title = titleText; // Internal UI state handling.
    contentWrapper.appendChild(title);

    const time = document.createElement('div');
    time.className = 'conversation-time';
    time.textContent = conversation._timeText || formatConversationTimestamp(conversation._time || new Date());
    contentWrapper.appendChild(time);

    item.appendChild(contentWrapper);

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'conversation-delete-btn';
    deleteBtn.innerHTML = `
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6h14zM10 11v6M14 11v6" 
                  stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
    `;
    deleteBtn.title = 'DeleteChat';
    deleteBtn.onclick = (e) => {
        e.stopPropagation();
        deleteConversation(conversation.id);
    };
    item.appendChild(deleteBtn);

    item.onclick = (e) => {
        e.preventDefault();
        e.stopPropagation();
        loadConversation(conversation.id);
    };
    return item;
}

// Internal UI state handling.
let conversationSearchTimer = null;
function handleConversationSearch(query) {
    // Internal UI state handling.
    if (conversationSearchTimer) {
        clearTimeout(conversationSearchTimer);
    }
    
    const searchInput = document.getElementById('conversation-search-input');
    const clearBtn = document.getElementById('conversation-search-clear');
    
    if (clearBtn) {
        if (query && query.trim()) {
            clearBtn.style.display = 'block';
        } else {
            clearBtn.style.display = 'none';
        }
    }
    
    conversationSearchTimer = setTimeout(() => {
        loadConversations(query);
    }, 300); // Internal UI state handling.
}

// Clear search
function clearConversationSearch() {
    const searchInput = document.getElementById('conversation-search-input');
    const clearBtn = document.getElementById('conversation-search-clear');
    
    if (searchInput) {
        searchInput.value = '';
    }
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    
    loadConversations('');
}

function formatConversationTimestamp(dateObj, todayStart, yesterdayStart) {
    if (!(dateObj instanceof Date) || isNaN(dateObj.getTime())) {
        return '';
    }
    // Internal UI state handling.
    const now = new Date();
    const referenceToday = todayStart || new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const referenceYesterday = yesterdayStart || new Date(referenceToday.getTime() - 24 * 60 * 60 * 1000);
    const messageDate = new Date(dateObj.getFullYear(), dateObj.getMonth(), dateObj.getDate());
    const fmtLocale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
    const yesterdayLabel = typeof window.t === 'function' ? window.t('chat.yesterday') : 'Copied';

    const timeOnlyOpts = { hour: '2-digit', minute: '2-digit' };
    const dateTimeOpts = { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' };
    const fullDateOpts = { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' };
    if (fmtLocale === 'zh-CN') {
        timeOnlyOpts.hour12 = false;
        dateTimeOpts.hour12 = false;
        fullDateOpts.hour12 = false;
    }
    if (messageDate.getTime() === referenceToday.getTime()) {
        return dateObj.toLocaleTimeString(fmtLocale, timeOnlyOpts);
    }
    if (messageDate.getTime() === referenceYesterday.getTime()) {
        return yesterdayLabel + ' ' + dateObj.toLocaleTimeString(fmtLocale, timeOnlyOpts);
    }
    if (dateObj.getFullYear() === referenceToday.getFullYear()) {
        return dateObj.toLocaleString(fmtLocale, dateTimeOpts);
    }
    return dateObj.toLocaleString(fmtLocale, fullDateOpts);
}

function getConversationGroup(dateObj, todayStart, sevenDaysCutoff, yesterdayStart) {
    if (!(dateObj instanceof Date) || isNaN(dateObj.getTime())) {
        return 'earlier';
    }
    const today = new Date(todayStart.getFullYear(), todayStart.getMonth(), todayStart.getDate());
    const yesterday = new Date(yesterdayStart.getFullYear(), yesterdayStart.getMonth(), yesterdayStart.getDate());
    const messageDay = new Date(dateObj.getFullYear(), dateObj.getMonth(), dateObj.getDate());

    if (messageDay.getTime() === today.getTime() || messageDay > today) {
        return 'today';
    }
    if (messageDay.getTime() === yesterday.getTime()) {
        return 'yesterday';
    }
    const cutoff = new Date(sevenDaysCutoff.getFullYear(), sevenDaysCutoff.getMonth(), sevenDaysCutoff.getDate());
    if (messageDay >= cutoff && messageDay < yesterday) {
        return 'last7Days';
    }
    return 'earlier';
}

// Internal UI state handling.
/** Internal UI state handling. */
async function prefetchLastAssistantProcessDetails() {
    const nodes = document.querySelectorAll('#chat-messages .message.assistant');
    if (!nodes.length) return;
    const last = nodes[nodes.length - 1];
    if (!last || !last.id) return;
    const container = document.getElementById('process-details-' + last.id);
    if (!container || container.dataset.lazyNotLoaded !== '1') return;
    const backendId = last.dataset && last.dataset.backendMessageId;
    if (!backendId || typeof apiFetch !== 'function') return;
    const res = await apiFetch('/api/messages/' + encodeURIComponent(String(backendId)) + '/process-details');
    const j = await res.json().catch(() => ({}));
    if (!res.ok || !Array.isArray(j.processDetails) || j.processDetails.length === 0) return;
    if (typeof renderProcessDetails === 'function') {
        renderProcessDetails(last.id, j.processDetails);
    }
    if (typeof window.expandProcessDetailsTimeline === 'function') {
        window.expandProcessDetailsTimeline(last.id);
    }
}

async function loadConversation(conversationId) {
    const seq = ++loadConversationRequestSeq;
    try {
        // Internal UI state handling.
        const response = await apiFetch(`/api/conversations/${conversationId}?include_process_details=0`);
        if (seq !== loadConversationRequestSeq) {
            return;
        }
        const conversation = await response.json();
        
        if (!response.ok) {
            showChatToast('ChatFailed: ' + (conversation.error || 'UnknownError'), 'error');
            return;
        }
        if (seq !== loadConversationRequestSeq) {
            return;
        }
        
        // Internal UI state handling.
        // Internal UI state handling.
        if (currentGroupId) {
            const sidebar = document.querySelector('.conversation-sidebar');
            const groupDetailPage = document.getElementById('group-detail-page');
            const chatContainer = document.querySelector('.chat-container');
            
            // Internal UI state handling.
            if (sidebar) sidebar.style.display = 'flex';
            // Internal UI state handling.
            if (groupDetailPage) groupDetailPage.style.display = 'none';
            if (chatContainer) chatContainer.style.display = 'flex';
            
            // Internal UI state handling.
            // Internal UI state handling.
            const previousGroupId = currentGroupId;
            currentGroupId = null;
            
            // Internal UI state handling.
            loadConversationsWithGroups();
        }
        
        // Internal UI state handling.
        // Internal UI state handling.
        if (Object.keys(conversationGroupMappingCache).length === 0) {
            await loadConversationGroupMapping();
        }
        if (seq !== loadConversationRequestSeq) {
            return;
        }
        currentConversationGroupId = conversationGroupMappingCache[conversationId] || null;

        // Internal UI state handling.
        loadGroups();
        
        // Internal UI state handling.
        currentConversationId = conversationId;
        window._loadedConversationProjectId = conversation.projectId || conversation.project_id || '';
        try {
            window.currentConversationId = conversationId;
        } catch (e) { /* ignore */ }
        if (typeof refreshChatProjectSelector === 'function') {
            refreshChatProjectSelector();
        }
        if (typeof window.syncHitlConfigFromServer === 'function') {
            await window.syncHitlConfigFromServer(conversationId);
        } else {
            refreshHitlConfigByCurrentConversation();
        }
        updateActiveConversation();
        
        // Internal UI state handling.
        const attackChainModal = document.getElementById('attack-chain-modal');
        if (attackChainModal && attackChainModal.style.display === 'block') {
            if (currentAttackChainConversationId !== conversationId) {
                closeAttackChainModal();
            }
        }
        
        // Internal UI state handling.
        const messagesDiv = document.getElementById('chat-messages');
        if (seq !== loadConversationRequestSeq) {
            return;
        }
        messagesDiv.innerHTML = '';
        
        // Internal UI state handling.
        let hasRecentUserMessage = false;
        if (conversation.messages && conversation.messages.length > 0) {
            const lastMessage = conversation.messages[conversation.messages.length - 1];
            if (lastMessage && lastMessage.role === 'user') {
                // Internal UI state handling.
                const messageTime = new Date(lastMessage.createdAt);
                const now = new Date();
                const timeDiff = now.getTime() - messageTime.getTime();
                if (timeDiff < 30000) { // Internal UI state handling.
                    hasRecentUserMessage = true;
                }
            }
        }
        if (hasRecentUserMessage) {
            // Internal UI state handling.
            clearChatDraft();
            const chatInput = document.getElementById('chat-input');
            if (chatInput) {
                chatInput.value = '';
                adjustTextareaHeight(chatInput);
            }
        }
        
        // Internal UI state handling.
        if (conversation.messages && conversation.messages.length > 0) {
            const FIRST_BATCH = 20;  // Internal UI state handling.
            const BATCH_SIZE = 10;   // Internal UI state handling.

            // Internal UI state handling.
            const renderOneMessage = (msg) => {
                if (msg.role === 'user' && isInterruptContinueInjectChatMessage(msg.content)) {
                    return;
                }
                let displayContent = msg.content;
                if (msg.role === 'assistant' && msg.content === 'Processing...' && msg.processDetails && msg.processDetails.length > 0) {
                    for (let i = msg.processDetails.length - 1; i >= 0; i--) {
                        const detail = msg.processDetails[i];
                        if (detail.eventType === 'error' || detail.eventType === 'cancelled') {
                            displayContent = detail.message || msg.content;
                            break;
                        }
                    }
                }

                // Internal UI state handling.
                // Internal UI state handling.
                // Internal UI state handling.
                const msgTime = (msg && msg.role === 'assistant' && msg.updatedAt) ? msg.updatedAt : (msg ? msg.createdAt : null);
                const messageId = addMessage(msg.role, displayContent, msg.mcpExecutionIds || [], null, msgTime);
                const messageEl = document.getElementById(messageId);
                if (messageEl && msg && msg.id) {
                    messageEl.dataset.backendMessageId = String(msg.id);
                    attachDeleteTurnButton(messageEl);
                }
                if (msg.role === 'assistant') {
                    const hasField = msg && Object.prototype.hasOwnProperty.call(msg, 'processDetails');
                    renderProcessDetails(messageId, hasField ? (msg.processDetails || []) : null);
                    if (msg.processDetails && msg.processDetails.length > 0) {
                        const hasErrorOrCancelled = msg.processDetails.some(d =>
                            d.eventType === 'error' || d.eventType === 'cancelled'
                        );
                        if (hasErrorOrCancelled) {
                            collapseAllProgressDetails(messageId, null);
                        }
                    }
                }
            };

            const msgs = conversation.messages;
            const firstBatch = msgs.slice(0, FIRST_BATCH);
            const rest = msgs.slice(FIRST_BATCH);

            let pendingMessageBatches = Promise.resolve();

            // Internal UI state handling.
            firstBatch.forEach(renderOneMessage);

            // Internal UI state handling.
            if (rest.length > 0) {
                const savedConvId = conversationId;
                const savedSeq = seq;
                pendingMessageBatches = new Promise((resolve) => {
                    let offset = 0;
                    const renderNextBatch = () => {
                        if (savedSeq !== loadConversationRequestSeq || currentConversationId !== savedConvId) {
                            resolve();
                            return;
                        }
                        const batch = rest.slice(offset, offset + BATCH_SIZE);
                        batch.forEach(renderOneMessage);
                        offset += BATCH_SIZE;
                        if (offset < rest.length) {
                            requestAnimationFrame(renderNextBatch);
                        } else {
                            if (window.CyberStrikeChatScroll) {
                                window.CyberStrikeChatScroll.forceScrollToBottom(false);
                            } else {
                                messagesDiv.scrollTop = messagesDiv.scrollHeight;
                            }
                            resolve();
                        }
                    };
                    requestAnimationFrame(renderNextBatch);
                });
            }

            if (window.CyberStrikeChatScroll) {
                window.CyberStrikeChatScroll.forceScrollToBottom(false);
            } else {
                messagesDiv.scrollTop = messagesDiv.scrollHeight;
            }
            addAttackChainButton(conversationId);
            await pendingMessageBatches;
            if (seq !== loadConversationRequestSeq) {
                return;
            }
            if (currentConversationId === conversationId && typeof window.restoreHitlInlineForConversation === 'function') {
                await window.restoreHitlInlineForConversation(conversationId);
            }
        } else {
            const readyMsgEmpty = typeof window.t === 'function' ? window.t('chat.systemReadyMessage') : 'System is ready. Automatic execution safety checks are enabled.';
            addMessage('assistant', readyMsgEmpty, null, null, null, { systemReadyMessage: true, scroll: 'force' });
            if (window.CyberStrikeChatScroll) {
                window.CyberStrikeChatScroll.forceScrollToBottom(false);
            } else {
                messagesDiv.scrollTop = messagesDiv.scrollHeight;
            }
            addAttackChainButton(conversationId);
            if (seq !== loadConversationRequestSeq) {
                return;
            }
            if (currentConversationId === conversationId && typeof window.restoreHitlInlineForConversation === 'function') {
                await window.restoreHitlInlineForConversation(conversationId);
            }
        }

        // Internal UI state handling.
        const skipReplay = typeof window.shouldSkipTaskEventReplayAttach === 'function'
            && window.shouldSkipTaskEventReplayAttach(conversationId);
        if (
            seq === loadConversationRequestSeq &&
            currentConversationId === conversationId &&
            typeof window.attachRunningTaskEventStream === 'function' &&
            !skipReplay
        ) {
            Promise.resolve()
                .then(() => window.attachRunningTaskEventStream(conversationId))
                .catch((e) => {
                    console.warn('attachRunningTaskEventStream on loadConversation failed', e);
                });
        } else if (seq === loadConversationRequestSeq && currentConversationId === conversationId) {
            // Internal UI state handling.
            prefetchLastAssistantProcessDetails().catch((e) => {
                console.warn('prefetchLastAssistantProcessDetails failed', e);
            });
        }
    } catch (error) {
        console.error('ChatFailed:', error);
        showChatToast('ChatFailed: ' + (error && error.message ? error.message : String(error)), 'error');
    }
}

/** Internal UI state handling. */
function attachDeleteTurnButton(messageEl) {
    if (!messageEl || !messageEl.dataset.backendMessageId) return;
    if (messageEl.querySelector('.message-delete-turn-btn')) return;
    const content = messageEl.querySelector('.message-content');
    if (!content) return;
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'message-delete-turn-btn';
    const title = typeof window.t === 'function' ? window.t('chat.deleteTurnTitle') : 'DeleteChat';
    btn.title = title;
    btn.setAttribute('aria-label', title);
    btn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2m3 0v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6h14zM10 11v6M14 11v6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>';
    btn.onclick = function (e) {
        e.stopPropagation();
        e.preventDefault();
        deleteConversationTurnFromUI(messageEl.dataset.backendMessageId);
    };
    const timeDiv = content.querySelector('.message-time');
    let footer = content.querySelector('.message-meta-footer');
    if (!footer && timeDiv && timeDiv.parentNode === content) {
        footer = document.createElement('div');
        footer.className = 'message-meta-footer';
        timeDiv.parentNode.insertBefore(footer, timeDiv);
        footer.appendChild(timeDiv);
    }
    if (footer) {
        footer.appendChild(btn);
    } else {
        content.appendChild(btn);
    }
}

/** Internal UI state handling. */
async function deleteConversationTurnFromUI(anchorBackendMessageId) {
    if (!currentConversationId || !anchorBackendMessageId) return;
    const confirmMsg = typeof window.t === 'function' ? window.t('chat.deleteTurnConfirm') : 'ConfirmDeleteChat?';
    if (!confirm(confirmMsg)) return;
    try {
        const response = await apiFetch(`/api/conversations/${currentConversationId}/delete-turn`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ messageId: anchorBackendMessageId })
        });
        let data = {};
        try {
            data = await response.json();
        } catch (e) { /* ignore */ }
        if (!response.ok) {
            throw new Error(data.error || data.message || 'delete failed');
        }
        await loadConversation(currentConversationId);
        if (typeof loadConversationsWithGroups === 'function') {
            loadConversationsWithGroups();
        } else if (typeof loadConversations === 'function') {
            loadConversations();
        }
    } catch (error) {
        console.error('delete turn failed:', error);
        const failed = typeof window.t === 'function' ? window.t('chat.deleteTurnFailed') : 'DeleteFailed';
        alert(failed + ': ' + (error && error.message ? error.message : error));
    }
}

// DeleteChat
async function deleteConversation(conversationId, skipConfirm = false) {
    // Internal UI state handling.
    if (!skipConfirm) {
        if (!confirm('Delete this conversation? This action cannot be undone.')) {
            return;
        }
    }
    
    try {
        const response = await apiFetch(`/api/conversations/${conversationId}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'DeleteFailed');
        }
        
        // Internal UI state handling.
        if (conversationId === currentConversationId) {
            currentConversationId = null;
            try {
                window.currentConversationId = '';
            } catch (e) { /* ignore */ }
            document.getElementById('chat-messages').innerHTML = '';
            const readyMsgLoad = typeof window.t === 'function' ? window.t('chat.systemReadyMessage') : 'System is ready. Automatic execution safety checks are enabled.';
            addMessage('assistant', readyMsgLoad, null, null, null, { systemReadyMessage: true });
            addAttackChainButton(null);
        }
        
        // Internal UI state handling.
        delete conversationGroupMappingCache[conversationId];
        // Internal UI state handling.
        delete pendingGroupMappings[conversationId];
        
        // Internal UI state handling.
        if (currentGroupId) {
            await loadGroupConversations(currentGroupId);
        }
        
        // Internal UI state handling.
        if (typeof loadConversationsWithGroups === 'function') {
            loadConversationsWithGroups();
        } else if (typeof loadConversations === 'function') {
            loadConversations();
        }
        // Internal UI state handling.
        try {
            document.dispatchEvent(new CustomEvent('conversation-deleted', { detail: { conversationId } }));
        } catch (e) { /* ignore */ }
    } catch (error) {
        console.error('DeleteChatFailed:', error);
        alert('DeleteChatFailed: ' + error.message);
    }
}

// Internal UI state handling.
function updateActiveConversation() {
    document.querySelectorAll('.conversation-item').forEach(item => {
        item.classList.remove('active');
        if (currentConversationId && item.dataset.conversationId === currentConversationId) {
            item.classList.add('active');
        }
    });
}

// Internal UI state handling.

// Internal UI state handling.
// Internal UI state handling.
function _acBuildNodeIconDataUrl(iconType, color, colorDark) {
    let iconPath = '';
    if (iconType === 'target') {
        iconPath = 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8zm0-14c-3.31 0-6 2.69-6 6s2.69 6 6 6 6-2.69 6-6-2.69-6-6-6zm0 10c-2.21 0-4-1.79-4-4s1.79-4 4-4 4 1.79 4 4-1.79 4-4 4z';
    } else if (iconType === 'action') {
        iconPath = 'M7 2v11h3v9l7-12h-4l4-8z';
    } else if (iconType === 'vulnerability') {
        iconPath = 'M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm-1 6h2v6h-2V7zm0 8h2v2h-2v-2z';
    } else {
        iconPath = 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z';
    }
    // Internal UI state handling.
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">
<defs>
<linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%">
<stop offset="0%" stop-color="${color}"/>
<stop offset="100%" stop-color="${colorDark}"/>
</linearGradient>
</defs>
<rect x="0" y="0" width="64" height="64" rx="14" fill="url(#g)"/>
<g transform="translate(14 14) scale(1.5)"><path d="${iconPath}" fill="#FFFFFF"/></g>
</svg>`;
    // Internal UI state handling.
    try {
        return 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(svg)));
    } catch (e) {
        // Internal UI state handling.
        return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
    }
}

let attackChainCytoscape = null;
let currentAttackChainConversationId = null;
// Internal UI state handling.
const attackChainLoadingMap = new Map(); // Map<conversationId, boolean>

// Internal UI state handling.
function isAttackChainLoading(conversationId) {
    return attackChainLoadingMap.get(conversationId) === true;
}

// Internal UI state handling.
function setAttackChainLoading(conversationId, loading) {
    if (loading) {
        attackChainLoadingMap.set(conversationId, true);
    } else {
        attackChainLoadingMap.delete(conversationId);
    }
}

// Internal UI state handling.
function addAttackChainButton(conversationId) {
    // Internal UI state handling.
    // Internal UI state handling.
    const conversationHeader = document.getElementById('conversation-header');
    if (conversationHeader) {
        conversationHeader.style.display = 'none';
    }
}

function updateAttackChainAvailability() {
    addAttackChainButton(currentConversationId);
}

// Internal UI state handling.
async function showAttackChain(conversationId) {
    // Internal UI state handling.
    // Internal UI state handling.
    if (isAttackChainLoading(conversationId) && currentAttackChainConversationId === conversationId) {
        // Internal UI state handling.
        const modal = document.getElementById('attack-chain-modal');
        if (modal && modal.style.display === 'block') {
            console.log('Attack chainLoading, text');
            return;
        }
    }
    
    currentAttackChainConversationId = conversationId;
    const modal = document.getElementById('attack-chain-modal');
    if (!modal) {
        console.error('Attack chain');
        return;
    }
    
    modal.style.display = 'block';
    // Internal UI state handling.
    updateAttackChainStats({ nodes: [], edges: [] });

    // Internal UI state handling.
    const container = document.getElementById('attack-chain-container');
    if (container) {
        container.innerHTML = '<div class="loading-spinner">' + (typeof window.t === 'function' ? window.t('chat.loading') : 'Loading...') + '</div>';
    }
    
    // Internal UI state handling.
    const detailsPanel = document.getElementById('attack-chain-details');
    if (detailsPanel) {
        detailsPanel.style.display = 'none';
    }
    
    // Internal UI state handling.
    const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
    if (regenerateBtn) {
        regenerateBtn.disabled = true;
        regenerateBtn.style.opacity = '0.5';
        regenerateBtn.style.cursor = 'not-allowed';
    }
    
    // Internal UI state handling.
    await loadAttackChain(conversationId);
}

// Internal UI state handling.
async function loadAttackChain(conversationId) {
    if (isAttackChainLoading(conversationId)) {
        return; // Internal UI state handling.
    }
    
    setAttackChainLoading(conversationId, true);
    
    try {
        const response = await apiFetch(`/api/attack-chain/${conversationId}`);
        
        if (!response.ok) {
            // Internal UI state handling.
            if (response.status === 409) {
                const error = await response.json();
                const container = document.getElementById('attack-chain-container');
                if (container) {
                    container.innerHTML = `
                        <div style="text-align: center; padding: 28px 24px; color: var(--text-secondary);">
                            <div style="display: inline-flex; align-items: center; gap: 8px; font-size: 0.95rem; color: var(--text-primary);">
                                <span role="presentation" aria-hidden="true">⏳</span>
                                <span>Attack chainGenerate, text</span>
                            </div>
                            <button class="btn-secondary" onclick="refreshAttackChain()" style="margin-top: 12px; font-size: 0.78rem; padding: 4px 12px;">
                                Refresh
                            </button>
                        </div>
                    `;
                }
                // Internal UI state handling.
                // Internal UI state handling.
                setTimeout(() => {
                    // Internal UI state handling.
                    if (currentAttackChainConversationId === conversationId) {
                        refreshAttackChain();
                    }
                }, 5000);
                // Internal UI state handling.
                // Internal UI state handling.
                // Internal UI state handling.
                // Internal UI state handling.
                const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
                if (regenerateBtn) {
                    regenerateBtn.disabled = false;
                    regenerateBtn.style.opacity = '1';
                    regenerateBtn.style.cursor = 'pointer';
                }
                return; // Internal UI state handling.
            }
            
            const error = await response.json();
            throw new Error(error.error || 'Attack chainFailed');
        }
        
        const chainData = await response.json();
        
        // Internal UI state handling.
        if (currentAttackChainConversationId !== conversationId) {
            console.log('Attack chain, Chat, text', {
                returned: conversationId,
                current: currentAttackChainConversationId
            });
            setAttackChainLoading(conversationId, false);
            return;
        }
        
        // Internal UI state handling.
        renderAttackChain(chainData);
        
        // Internal UI state handling.
        updateAttackChainStats(chainData);
        
        // Internal UI state handling.
        setAttackChainLoading(conversationId, false);
        
    } catch (error) {
        console.error('Attack chainFailed:', error);
        const container = document.getElementById('attack-chain-container');
        if (container) {
            container.innerHTML = '<div class="error-message">' + (typeof window.t === 'function' ? window.t('chat.loadFailed', { message: error.message }) : 'Failed: ' + error.message) + '</div>';
        }
        // Internal UI state handling.
        setAttackChainLoading(conversationId, false);
    } finally {
        // Internal UI state handling.
        const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
        if (regenerateBtn) {
            regenerateBtn.disabled = false;
            regenerateBtn.style.opacity = '1';
            regenerateBtn.style.cursor = 'pointer';
        }
    }
}

// Internal UI state handling.
function renderAttackChain(chainData) {
    const container = document.getElementById('attack-chain-container');
    if (!container) {
        return;
    }
    
    // Internal UI state handling.
    container.innerHTML = '';
    
    if (!chainData.nodes || chainData.nodes.length === 0) {
        container.innerHTML = '<div class="empty-message">' + (typeof window.t === 'function' ? window.t('chat.noAttackChainData') : 'NoneAttack chain') + '</div>';
        return;
    }
    
    // Internal UI state handling.
    const nodeCount = chainData.nodes.length;
    const edgeCount = chainData.edges.length;
    const isComplexGraph = nodeCount > 15 || edgeCount > 25;
    
    // Internal UI state handling.
    chainData.nodes.forEach(node => {
        if (node.label) {
            // Internal UI state handling.
            const maxLength = isComplexGraph ? 18 : 22;
            if (node.label.length > maxLength) {
                let truncated = node.label.substring(0, maxLength);
                // Internal UI state handling.
                const lastPunct = Math.max(
                    truncated.lastIndexOf(', '),
                    truncated.lastIndexOf('.'),
                    truncated.lastIndexOf(','),
                    truncated.lastIndexOf(' '),
                    truncated.lastIndexOf('/')
                );
                if (lastPunct > maxLength * 0.6) { // Internal UI state handling.
                    truncated = truncated.substring(0, lastPunct + 1);
                }
                node.label = truncated + '...';
            }
        }
    });
    
    // Internal UI state handling.
    const elements = [];
    
    // Internal UI state handling.
    chainData.nodes.forEach(node => {
        const riskScore = node.risk_score || 0;
        const nodeType = node.type || '';
        const metadata = node.metadata || {};

        // Internal UI state handling.
        let typeLabel = 'Node';
        let typeEn = 'NODE';
        let typeColor = '#334155';      // Internal UI state handling.
        let accentColor = '#94a3b8';    // Internal UI state handling.
        let accentDark = '#475569';     // Internal UI state handling.
        let bgGradientStart = '#FFFFFF';
        let bgGradientEnd = '#F8FAFC';
        let iconType = 'default';       // Type

        if (nodeType === 'target') {
            typeLabel = 'Node';
            typeEn = 'TARGET';
            typeColor = '#312E81';
            accentColor = '#4F46E5';
            accentDark = '#3730A3';
            bgGradientStart = '#FFFFFF';
            bgGradientEnd = '#F5F3FF';
            iconType = 'target';
        } else if (nodeType === 'action') {
            typeLabel = 'Node';
            typeEn = 'ACTION';
            const findings = metadata.findings || [];
            const hasFindings = Array.isArray(findings) && findings.length > 0;
            const isFailedInsight = (metadata.status || '') === 'failed_insight';
            if (hasFindings && !isFailedInsight) {
                typeColor = '#064E3B';
                accentColor = '#10B981';
                accentDark = '#047857';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#ECFDF5';
            } else {
                typeColor = '#334155';
                accentColor = '#64748B';
                accentDark = '#475569';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#F8FAFC';
            }
            iconType = 'action';
        } else if (nodeType === 'vulnerability') {
            typeLabel = 'Node';
            typeEn = 'VULNERABILITY';
            if (riskScore >= 80) {
                typeColor = '#881337';
                accentColor = '#E11D48';
                accentDark = '#BE123C';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#FFF1F2';
            } else if (riskScore >= 60) {
                typeColor = '#7C2D12';
                accentColor = '#EA580C';
                accentDark = '#C2410C';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#FFF7ED';
            } else if (riskScore >= 40) {
                typeColor = '#713F12';
                accentColor = '#CA8A04';
                accentDark = '#A16207';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#FEFCE8';
            } else {
                typeColor = '#134E4A';
                accentColor = '#0D9488';
                accentDark = '#0F766E';
                bgGradientStart = '#FFFFFF';
                bgGradientEnd = '#F0FDFA';
            }
            iconType = 'vulnerability';
        }

        // Internal UI state handling.
        const iconSvg = _acBuildNodeIconDataUrl(iconType, accentColor, accentDark);

        // Internal UI state handling.
        let badgeText = '';
        if (nodeType === 'vulnerability' && riskScore > 0) {
            const rl = riskScore >= 80 ? 'Critical' : riskScore >= 60 ? 'High' : riskScore >= 40 ? 'Medium' : 'Low';
            badgeText = rl + ' · ' + riskScore;
        } else if (nodeType === 'action') {
            const findings = metadata.findings || [];
            if (Array.isArray(findings) && findings.length > 0 && metadata.status !== 'failed_insight') {
                badgeText = 'Found ' + findings.length;
            } else if (metadata.status === 'failed_insight') {
                badgeText = 'Copied';
            }
        } else if (nodeType === 'target') {
            badgeText = 'Copied';
        }

        elements.push({
            data: {
                id: node.id,
                label: node.label,
                originalLabel: node.label,
                type: nodeType,
                typeLabel: typeLabel,
                typeEn: typeEn,
                typeColor: typeColor,
                accentColor: accentColor,
                accentDark: accentDark,
                bgGradientStart: bgGradientStart,
                bgGradientEnd: bgGradientEnd,
                iconDataUrl: iconSvg,
                badgeText: badgeText,
                riskScore: riskScore,
                toolExecutionId: node.tool_execution_id || '',
                metadata: metadata
            }
        });
    });
    
    // Internal UI state handling.
    const nodeIds = new Set(chainData.nodes.map(node => node.id));
    
    // Internal UI state handling.
    const validEdges = [];
    chainData.edges.forEach(edge => {
        // Internal UI state handling.
        if (nodeIds.has(edge.source) && nodeIds.has(edge.target)) {
            validEdges.push(edge);
            elements.push({
                data: {
                    id: edge.id,
                    source: edge.source,
                    target: edge.target,
                    type: edge.type || 'leads_to',
                    weight: edge.weight || 1
                }
            });
        } else {
            console.warn('Invalid edge', {
                edgeId: edge.id,
                source: edge.source,
                target: edge.target,
                sourceExists: nodeIds.has(edge.source),
                targetExists: nodeIds.has(edge.target)
            });
        }
    });
    
    // Internal UI state handling.
    attackChainCytoscape = cytoscape({
        container: container,
        elements: elements,
        style: [
            {
                selector: 'node',
                style: {
                    // Internal UI state handling.
                    'label': function(ele) {
                        const typeEn = ele.data('typeEn') || '';
                        const typeLabel = ele.data('typeLabel') || '';
                        const label = ele.data('label') || '';
                        const badgeText = ele.data('badgeText') || '';
                        // Internal UI state handling.
                        // Internal UI state handling.
                        // Internal UI state handling.
                        let line1 = typeEn + '  ·  ' + typeLabel;
                        if (badgeText) line1 += '  [' + badgeText + ']';
                        return line1 + '\n' + label;
                    },
                    'width': function(ele) {
                        const type = ele.data('type');
                        if (type === 'target') return isComplexGraph ? 300 : 360;
                        if (type === 'vulnerability') return isComplexGraph ? 280 : 340;
                        return isComplexGraph ? 260 : 320;
                    },
                    'height': function(ele) {
                        return isComplexGraph ? 84 : 100;
                    },
                    'shape': 'round-rectangle',
                    // Internal UI state handling.
                    'background-fill': 'linear-gradient',
                    'background-gradient-direction': 'to-bottom-right',
                    'background-gradient-stop-colors': function(ele) {
                        return (ele.data('bgGradientStart') || '#FFFFFF') + ' ' +
                               (ele.data('bgGradientEnd') || '#F8FAFC');
                    },
                    'background-gradient-stop-positions': '0 100',
                    'background-opacity': 1,
                    // Internal UI state handling.
                    'background-image': function(ele) {
                        return ele.data('iconDataUrl') || 'none';
                    },
                    'background-image-containment': 'inside',
                    'background-fit': 'none',
                    'background-image-opacity': 1,
                    'background-width': '36px',
                    'background-height': '36px',
                    'background-position-x': '18px',
                    'background-position-y': '50%',
                    'background-offset-y': '0',
                    'background-clip': 'node',
                    'bounds-expansion': 0,
                    // Internal UI state handling.
                    'border-width': 1.5,
                    'border-color': function(ele) {
                        return ele.data('accentColor') || '#94a3b8';
                    },
                    'border-opacity': 0.5,
                    // Internal UI state handling.
                    'color': '#0f172a',
                    'font-size': function(ele) {
                        return isComplexGraph ? '13px' : '14px';
                    },
                    'font-weight': 700,
                    'font-family': '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", "PingFang SC", "Microsoft YaHei", sans-serif',
                    // Internal UI state handling.
                    'text-valign': 'center',
                    'text-halign': 'center',
                    'text-justification': 'left',
                    'text-wrap': 'wrap',
                    'text-max-width': function(ele) {
                        const type = ele.data('type');
                        const w = (type === 'target') ? (isComplexGraph ? 300 : 360)
                                : (type === 'vulnerability') ? (isComplexGraph ? 280 : 340)
                                : (isComplexGraph ? 260 : 320);
                        return (w - 80) + 'px';
                    },
                    'text-overflow-wrap': 'anywhere',
                    'text-margin-x': 28,
                    'text-margin-y': 0,
                    'padding': '12px',
                    'line-height': 1.4,
                    'text-outline-width': 0,
                    // Internal UI state handling.
                    'overlay-color': '#0f172a',
                    'overlay-opacity': 0,
                    'overlay-padding': 0,
                    'transition-property': 'overlay-opacity, border-width, border-color',
                    'transition-duration': '160ms'
                }
            },
            {
                // Internal UI state handling.
                selector: 'node[type = "target"]',
                style: {
                    'border-width': 2
                }
            },
            {
                // Internal UI state handling.
                selector: 'node[type = "vulnerability"]',
                style: {
                    'border-width': 2
                }
            },
            {
                selector: 'edge',
                style: {
                    'width': function(ele) {
                        const type = ele.data('type');
                        if (type === 'discovers') return 2.6;
                        if (type === 'enables') return 2.8;
                        return 2;
                    },
                    'line-color': function(ele) {
                        const type = ele.data('type');
                        if (type === 'discovers' || type === 'targets') return '#4F46E5';
                        if (type === 'enables') return '#E11D48';
                        if (type === 'leads_to') return '#64748B';
                        return '#cbd5e1';
                    },
                    'target-arrow-color': function(ele) {
                        const type = ele.data('type');
                        if (type === 'discovers' || type === 'targets') return '#4F46E5';
                        if (type === 'enables') return '#E11D48';
                        if (type === 'leads_to') return '#64748B';
                        return '#cbd5e1';
                    },
                    'target-arrow-shape': 'triangle-backcurve',
                    'arrow-scale': 1.35,
                    'curve-style': 'bezier',
                    'control-point-step-size': 60,
                    'opacity': 0.88,
                    'line-style': function(ele) {
                        const type = ele.data('type');
                        if (type === 'targets') return 'dashed';
                        return 'solid';
                    },
                    'line-dash-pattern': function(ele) {
                        const type = ele.data('type');
                        if (type === 'targets') return [10, 5];
                        return [];
                    },
                    'transition-property': 'opacity, width, line-color',
                    'transition-duration': '160ms'
                }
            },
            {
                selector: 'node:selected',
                style: {
                    'border-width': 3.5,
                    'border-color': '#4F46E5',
                    'z-index': 999,
                    'opacity': 1,
                    'overlay-opacity': 0.06,
                    'overlay-color': '#4F46E5',
                    'overlay-padding': 8
                }
            }
        ],
        userPanningEnabled: true,
        userZoomingEnabled: true,
        boxSelectionEnabled: true,
        minZoom: 0.2,
        maxZoom: 3
    });
    
    // Internal UI state handling.
    let layoutOptions = {
        name: 'breadthfirst',
        directed: true,
        spacingFactor: isComplexGraph ? 3.0 : 2.5,
        padding: 40
    };
    
    // Internal UI state handling.
    // Internal UI state handling.
    let elkInstance = null;
    if (typeof ELK !== 'undefined') {
        try {
            elkInstance = new ELK();
        } catch (e) {
            console.warn('ELKFailed:', e);
        }
    }
    
    if (elkInstance) {
        try {
            
            // Internal UI state handling.
            const isSmallGraph = chainData.nodes.length <= 8 && validEdges.length <= 12;
            // Internal UI state handling.
            const nodeGap = isComplexGraph ? 45 : isSmallGraph ? 80 : 60;
            // Internal UI state handling.
            const layerGap = isComplexGraph ? 70 : isSmallGraph ? 130 : 95;

            // Internal UI state handling.
            const elkGraph = {
                id: 'root',
                layoutOptions: {
                    'elk.algorithm': 'layered',
                    'elk.direction': 'DOWN',
                    'elk.padding': '[top=30,left=50,bottom=30,right=50]',
                    'elk.spacing.nodeNode': String(nodeGap),
                    'elk.spacing.edgeNode': '20',
                    'elk.spacing.edgeEdge': '12',
                    'elk.spacing.componentComponent': '50',
                    'elk.layered.spacing.nodeNodeBetweenLayers': String(layerGap),
                    'elk.layered.spacing.edgeNodeBetweenLayers': '20',
                    'elk.layered.spacing.edgeEdgeBetweenLayers': '12',
                    'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
                    'elk.layered.nodePlacement.bk.fixedAlignment': 'BALANCED',
                    'elk.layered.nodePlacement.bk.edgeStraightening': 'IMPROVE_STRAIGHTNESS',
                    'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
                    'elk.layered.crossingMinimization.semiInteractive': 'false',
                    'elk.layered.thoroughness': String(isComplexGraph ? 10 : 15),
                    'elk.layered.cycleBreaking.strategy': 'GREEDY',
                    'elk.layered.compaction.connectedComponents': 'true',
                    'elk.layered.compaction.postCompaction.strategy': 'LEFT_RIGHT_CONSTRAINT_LOCKING',
                    'elk.layered.unnecessaryBendpoints': 'true',
                    'elk.layered.mergeEdges': 'false'
                },
                children: chainData.nodes.map(node => {
                    const type = node.type || '';
                    return {
                        id: node.id,
                        width: type === 'target' ? (isComplexGraph ? 300 : 360) :
                               type === 'vulnerability' ? (isComplexGraph ? 280 : 340) :
                               (isComplexGraph ? 260 : 320),
                        height: isComplexGraph ? 84 : 100
                    };
                }),
                edges: validEdges.map(edge => ({
                    id: edge.id,
                    sources: [edge.source],
                    targets: [edge.target]
                }))
            };
            
            // Internal UI state handling.
            elkInstance.layout(elkGraph).then(laidOutGraph => {
                // Internal UI state handling.
                if (laidOutGraph && laidOutGraph.children) {
                    laidOutGraph.children.forEach(elkNode => {
                        const cyNode = attackChainCytoscape.getElementById(elkNode.id);
                        if (cyNode && elkNode.x !== undefined && elkNode.y !== undefined) {
                            cyNode.position({
                                x: elkNode.x + (elkNode.width || 0) / 2,
                                y: elkNode.y + (elkNode.height || 0) / 2
                            });
                        }
                    });
                    
                    // Internal UI state handling.
                    setTimeout(() => {
                        centerAttackChain();
                    }, 150);
                } else {
                    throw new Error('ELK layout');
                }
            }).catch(err => {
                console.warn('ELK failed, using default layout:', err);
                // Internal UI state handling.
                const layout = attackChainCytoscape.layout(layoutOptions);
                layout.one('layoutstop', () => {
                    setTimeout(() => {
                        centerAttackChain();
                    }, 100);
                });
                layout.run();
            });
        } catch (e) {
            console.warn('ELK failed, using default layout:', e);
            // Internal UI state handling.
            const layout = attackChainCytoscape.layout(layoutOptions);
            layout.one('layoutstop', () => {
                setTimeout(() => {
                    centerAttackChain();
                }, 100);
            });
            layout.run();
        }
    } else {
        console.warn('ELK.js is not loaded; using default layout. Check that the elkjs library is loaded.');
        // Internal UI state handling.
        const layout = attackChainCytoscape.layout(layoutOptions);
        layout.one('layoutstop', () => {
            setTimeout(() => {
                centerAttackChain();
            }, 100);
        });
        layout.run();
    }
    
    // Internal UI state handling.
    function centerAttackChain() {
        try {
            if (!attackChainCytoscape) {
                return;
            }
            const container = attackChainCytoscape.container();
            if (!container) return;
            const containerWidth = container.offsetWidth;
            const containerHeight = container.offsetHeight;
            if (containerWidth === 0 || containerHeight === 0) {
                setTimeout(centerAttackChain, 100);
                return;
            }

            // Internal UI state handling.
            // Internal UI state handling.
            const padding = 60;
            attackChainCytoscape.fit(undefined, padding);

            // Internal UI state handling.
            setTimeout(() => {
                if (!attackChainCytoscape) return;
                const currentZoom = attackChainCytoscape.zoom();
                // Internal UI state handling.
                const MAX_INITIAL_ZOOM = 1.25;
                // Internal UI state handling.
                const MIN_READABLE_ZOOM = 0.25;

                let targetZoom = currentZoom;
                if (currentZoom > MAX_INITIAL_ZOOM) {
                    targetZoom = MAX_INITIAL_ZOOM;
                } else if (currentZoom < MIN_READABLE_ZOOM) {
                    // Internal UI state handling.
                    targetZoom = MIN_READABLE_ZOOM;
                }

                if (Math.abs(targetZoom - currentZoom) > 0.01) {
                    const extent = attackChainCytoscape.extent();
                    const cx = (extent.x1 + extent.x2) / 2;
                    const cy = (extent.y1 + extent.y2) / 2;
                    attackChainCytoscape.zoom({
                        level: targetZoom,
                        position: { x: cx, y: cy }
                    });
                }
                attackChainCytoscape.center();
            }, 60);
        } catch (error) {
            console.warn('Error:', error);
        }
    }
    
    // Internal UI state handling.
    attackChainCytoscape.on('tap', 'node', function(evt) {
        const node = evt.target;
        showNodeDetails(node.data());
    });

    // Internal UI state handling.
    attackChainCytoscape.on('tap', function(evt) {
        if (evt.target === attackChainCytoscape) {
            attackChainCytoscape.elements().unselect();
        }
    });

    // Internal UI state handling.
    attackChainCytoscape.on('mouseover', 'node', function(evt) {
        const node = evt.target;
        const accent = node.data('accentColor') || '#4F46E5';
        node.style({
            'border-width': 3,
            'border-color': accent,
            'border-opacity': 1,
            'overlay-color': accent,
            'overlay-opacity': 0.08,
            'overlay-padding': 10,
            'z-index': 998
        });
        const connected = node.connectedEdges();
        attackChainCytoscape.edges().not(connected).style('opacity', 0.2);
        connected.style({ 'opacity': 1, 'width': 3.5 });
    });

    attackChainCytoscape.on('mouseout', 'node', function(evt) {
        const node = evt.target;
        const type = node.data('type');
        const defaultBorderWidth = (type === 'target' || type === 'vulnerability') ? 2 : 1.5;
        node.style({
            'border-width': defaultBorderWidth,
            'border-color': node.data('accentColor') || '#94a3b8',
            'border-opacity': 0.5,
            'overlay-opacity': 0,
            'overlay-padding': 0,
            'z-index': 0
        });
        attackChainCytoscape.edges().style({ 'opacity': 0.88, 'width': '' });
    });

    // Internal UI state handling.
    window.attackChainOriginalData = chainData;
}

// Internal UI state handling.
function getEdgeNodes(edge) {
    try {
        const source = edge.source();
        const target = edge.target();
        
        // Internal UI state handling.
        if (!source || !target || source.length === 0 || target.length === 0) {
            return { source: null, target: null, valid: false };
        }
        
        return { source: source, target: target, valid: true };
    } catch (error) {
        console.warn('Error:', error, edge.id());
        return { source: null, target: null, valid: false };
    }
}

// Internal UI state handling.
function filterAttackChainNodes(searchText) {
    if (!attackChainCytoscape || !window.attackChainOriginalData) {
        return;
    }
    
    const searchLower = searchText.toLowerCase().trim();
    if (searchLower === '') {
        // Internal UI state handling.
        attackChainCytoscape.nodes().style('display', 'element');
        attackChainCytoscape.edges().style('display', 'element');
        // Internal UI state handling.
        attackChainCytoscape.nodes().style('border-width', 2);
        return;
    }
    
    // Internal UI state handling.
    attackChainCytoscape.nodes().forEach(node => {
        // Internal UI state handling.
        const originalLabel = node.data('originalLabel') || node.data('label') || '';
        const label = originalLabel.toLowerCase();
        const type = (node.data('type') || '').toLowerCase();
        const matches = label.includes(searchLower) || type.includes(searchLower);
        
        if (matches) {
            node.style('display', 'element');
            // Internal UI state handling.
            node.style('border-width', 4);
            node.style('border-color', '#0066ff');
        } else {
            node.style('display', 'none');
        }
    });
    
    // Internal UI state handling.
    attackChainCytoscape.edges().forEach(edge => {
        const { source, target, valid } = getEdgeNodes(edge);
        if (!valid) {
            edge.style('display', 'none');
            return;
        }
        
        const sourceVisible = source.style('display') !== 'none';
        const targetVisible = target.style('display') !== 'none';
        if (sourceVisible && targetVisible) {
            edge.style('display', 'element');
        } else {
            edge.style('display', 'none');
        }
    });
    
    // Internal UI state handling.
    attackChainCytoscape.fit(undefined, 60);
}

// Internal UI state handling.
function filterAttackChainByType(type) {
    if (!attackChainCytoscape || !window.attackChainOriginalData) {
        return;
    }
    
    if (type === 'all') {
        attackChainCytoscape.nodes().style('display', 'element');
        attackChainCytoscape.edges().style('display', 'element');
        attackChainCytoscape.nodes().style('border-width', 2);
        attackChainCytoscape.fit(undefined, 60);
        return;
    }
    
    // Internal UI state handling.
    attackChainCytoscape.nodes().forEach(node => {
        const nodeType = node.data('type') || '';
        if (nodeType === type) {
            node.style('display', 'element');
        } else {
            node.style('display', 'none');
        }
    });
    
    // Internal UI state handling.
    attackChainCytoscape.edges().forEach(edge => {
        const { source, target, valid } = getEdgeNodes(edge);
        if (!valid) {
            edge.style('display', 'none');
            return;
        }
        
        const sourceVisible = source.style('display') !== 'none';
        const targetVisible = target.style('display') !== 'none';
        if (sourceVisible && targetVisible) {
            edge.style('display', 'element');
        } else {
            edge.style('display', 'none');
        }
    });
    
    // Internal UI state handling.
    attackChainCytoscape.fit(undefined, 60);
}

// Internal UI state handling.
function filterAttackChainByRisk(riskLevel) {
    if (!attackChainCytoscape || !window.attackChainOriginalData) {
        return;
    }
    
    if (riskLevel === 'all') {
        attackChainCytoscape.nodes().style('display', 'element');
        attackChainCytoscape.edges().style('display', 'element');
        attackChainCytoscape.nodes().style('border-width', 2);
        attackChainCytoscape.fit(undefined, 60);
        return;
    }
    
    // Internal UI state handling.
    const riskRanges = {
        'high': [80, 100],
        'medium-high': [60, 79],
        'medium': [40, 59],
        'low': [0, 39]
    };
    
    const [minRisk, maxRisk] = riskRanges[riskLevel] || [0, 100];
    
    // Internal UI state handling.
    attackChainCytoscape.nodes().forEach(node => {
        const riskScore = node.data('riskScore') || 0;
        if (riskScore >= minRisk && riskScore <= maxRisk) {
            node.style('display', 'element');
        } else {
            node.style('display', 'none');
        }
    });
    
    // Internal UI state handling.
    attackChainCytoscape.edges().forEach(edge => {
        const { source, target, valid } = getEdgeNodes(edge);
        if (!valid) {
            edge.style('display', 'none');
            return;
        }
        
        const sourceVisible = source.style('display') !== 'none';
        const targetVisible = target.style('display') !== 'none';
        if (sourceVisible && targetVisible) {
            edge.style('display', 'element');
        } else {
            edge.style('display', 'none');
        }
    });
    
    // Internal UI state handling.
    attackChainCytoscape.fit(undefined, 60);
}

// ResetAttack chainFilter
function resetAttackChainFilters() {
    // Internal UI state handling.
    const searchInput = document.getElementById('attack-chain-search');
    if (searchInput) {
        searchInput.value = '';
    }
    
    // ResetTypeFilter
    const typeFilter = document.getElementById('attack-chain-type-filter');
    if (typeFilter) {
        typeFilter.value = 'all';
    }
    
    // Internal UI state handling.
    const riskFilter = document.getElementById('attack-chain-risk-filter');
    if (riskFilter) {
        riskFilter.value = 'all';
    }
    
    // Internal UI state handling.
    if (attackChainCytoscape) {
        attackChainCytoscape.nodes().forEach(node => {
            node.style('display', 'element');
            node.style('border-width', 2); // Internal UI state handling.
        });
        attackChainCytoscape.edges().style('display', 'element');
        attackChainCytoscape.fit(undefined, 60);
    }
}

// Internal UI state handling.
function showNodeDetails(nodeData) {
    const detailsPanel = document.getElementById('attack-chain-details');
    const detailsContent = document.getElementById('attack-chain-details-content');
    
    if (!detailsPanel || !detailsContent) {
        return;
    }

    // Internal UI state handling.
    const sidebar = document.querySelector('.attack-chain-sidebar');
    if (sidebar) sidebar.classList.add('details-active');

    // Internal UI state handling.
    requestAnimationFrame(() => {
        detailsPanel.style.display = 'flex';
        requestAnimationFrame(() => {
            detailsPanel.style.opacity = '1';
        });
    });
    
    let html = `
        <div class="node-detail-item">
            <strong>Node ID:</strong> <code>${nodeData.id}</code>
        </div>
        <div class="node-detail-item">
            <strong>Type:</strong> ${getNodeTypeLabel(nodeData.type)}
        </div>
        <div class="node-detail-item">
            <strong>Label:</strong> ${escapeHtml(nodeData.originalLabel || nodeData.label)}
        </div>
        <div class="node-detail-item">
            <strong>Risk score:</strong> ${nodeData.riskScore}/100
        </div>
    `;
    
    // Internal UI state handling.
    if (nodeData.type === 'action' && nodeData.metadata) {
        if (nodeData.metadata.tool_name) {
            html += `
                <div class="node-detail-item">
                    <strong>tools:</strong> <code>${escapeHtml(nodeData.metadata.tool_name)}</code>
                </div>
            `;
        }
        if (nodeData.metadata.tool_intent) {
            html += `
                <div class="node-detail-item">
                    <strong>tools:</strong> <span style="color: #0066ff; font-weight: bold;">${escapeHtml(nodeData.metadata.tool_intent)}</span>
                </div>
            `;
        }
        if (nodeData.metadata.status === 'failed_insight') {
            html += `
                <div class="node-detail-item">
                    <strong>ExecuteStatus:</strong> <span style="color: #ff9800; font-weight: bold;">Failed</span>
                </div>
            `;
        }
        if (nodeData.metadata.ai_analysis) {
            html += `
                <div class="node-detail-item">
                    <strong>AI analysis:</strong> <div style="margin-top: 5px; padding: 8px; background: #f5f5f5; border-radius: 4px;">${escapeHtml(nodeData.metadata.ai_analysis)}</div>
                </div>
            `;
        }
        if (nodeData.metadata.findings && Array.isArray(nodeData.metadata.findings) && nodeData.metadata.findings.length > 0) {
            html += `
                <div class="node-detail-item">
                    <strong>Key findings:</strong>
                    <ul style="margin: 5px 0; padding-left: 20px;">
                        ${nodeData.metadata.findings.map(f => `<li>${escapeHtml(f)}</li>`).join('')}
                    </ul>
                </div>
            `;
        }
    }
    
    // Internal UI state handling.
    if (nodeData.type === 'target' && nodeData.metadata && nodeData.metadata.target) {
        html += `
            <div class="node-detail-item">
                <strong>Target:</strong> <code>${escapeHtml(nodeData.metadata.target)}</code>
            </div>
        `;
    }
    
    // Internal UI state handling.
    if (nodeData.type === 'vulnerability' && nodeData.metadata) {
        if (nodeData.metadata.vulnerability_type) {
            html += `
                <div class="node-detail-item">
                    <strong>Type:</strong> ${escapeHtml(nodeData.metadata.vulnerability_type)}
                </div>
            `;
        }
        if (nodeData.metadata.description) {
            html += `
                <div class="node-detail-item">
                    <strong>Description:</strong> ${escapeHtml(nodeData.metadata.description)}
                </div>
            `;
        }
        if (nodeData.metadata.severity) {
            html += `
                <div class="node-detail-item">
                    <strong>Critical:</strong> <span style="color: ${getSeverityColor(nodeData.metadata.severity)}; font-weight: bold;">${escapeHtml(nodeData.metadata.severity)}</span>
                </div>
            `;
        }
        if (nodeData.metadata.location) {
            html += `
                <div class="node-detail-item">
                    <strong>Location:</strong> <code>${escapeHtml(nodeData.metadata.location)}</code>
                </div>
            `;
        }
    }
    
    if (nodeData.toolExecutionId) {
        html += `
            <div class="node-detail-item">
                <strong>toolsExecution ID:</strong> <code>${nodeData.toolExecutionId}</code>
            </div>
        `;
    }
    
    // Internal UI state handling.
    if (detailsContent) {
        detailsContent.scrollTop = 0;
    }

    requestAnimationFrame(() => {
        detailsContent.innerHTML = html;
        requestAnimationFrame(() => {
            if (detailsContent) {
                detailsContent.scrollTop = 0;
            }
        });
    });
}

// Internal UI state handling.
function getSeverityColor(severity) {
    const colors = {
        'critical': '#ff0000',
        'high': '#ff4444',
        'medium': '#ff8800',
        'low': '#ffbb00'
    };
    return colors[severity.toLowerCase()] || '#666';
}

// Internal UI state handling.
function getNodeTypeLabel(type) {
    const labels = {
        'action': 'Copied',
        'vulnerability': 'Copied',
        'target': 'Copied'
    };
    return labels[type] || type;
}

// Internal UI state handling.
function updateAttackChainStats(chainData) {
    const statsElement = document.getElementById('attack-chain-stats');
    if (statsElement) {
        const nodeCount = chainData.nodes ? chainData.nodes.length : 0;
        const edgeCount = chainData.edges ? chainData.edges.length : 0;
        if (typeof window.t === 'function') {
            statsElement.textContent = window.t('attackChainModal.nodesEdges', {
                nodes: nodeCount,
                edges: edgeCount
            });
        } else {
            statsElement.textContent = `Nodes: ${nodeCount} | Edges: ${edgeCount}`;
        }
    }
}

// Internal UI state handling.
document.addEventListener('languagechange', function () {
    if (window.attackChainOriginalData && typeof updateAttackChainStats === 'function') {
        updateAttackChainStats(window.attackChainOriginalData);
    } else {
        const statsEl = document.getElementById('attack-chain-stats');
        if (statsEl && typeof window.t === 'function') {
            statsEl.textContent = window.t('attackChainModal.nodesEdges', { nodes: 0, edges: 0 });
        }
    }
});

// Internal UI state handling.
function closeNodeDetails() {
    const detailsPanel = document.getElementById('attack-chain-details');
    const sidebar = document.querySelector('.attack-chain-sidebar');

    if (detailsPanel) {
        detailsPanel.style.opacity = '0';
        setTimeout(() => {
            detailsPanel.style.display = 'none';
            detailsPanel.style.opacity = '';
            // Internal UI state handling.
            if (sidebar) sidebar.classList.remove('details-active');
        }, 220);
    } else if (sidebar) {
        sidebar.classList.remove('details-active');
    }

    if (attackChainCytoscape) {
        attackChainCytoscape.elements().unselect();
    }
}

// Internal UI state handling.
function closeAttackChainModal() {
    const modal = document.getElementById('attack-chain-modal');
    if (modal) {
        modal.style.display = 'none';
    }
    
    // Internal UI state handling.
    closeNodeDetails();
    
    // Internal UI state handling.
    if (attackChainCytoscape) {
        attackChainCytoscape.destroy();
        attackChainCytoscape = null;
    }
    
    currentAttackChainConversationId = null;
}

// Internal UI state handling.
// Internal UI state handling.
function refreshAttackChain() {
    if (currentAttackChainConversationId) {
        // Internal UI state handling.
        const wasLoading = isAttackChainLoading(currentAttackChainConversationId);
        setAttackChainLoading(currentAttackChainConversationId, false); // Internal UI state handling.
        loadAttackChain(currentAttackChainConversationId).finally(() => {
            // Internal UI state handling.
            // Internal UI state handling.
            if (wasLoading) {
                // Internal UI state handling.
                // Internal UI state handling.
                // Internal UI state handling.
            }
        });
    }
}

// Internal UI state handling.
async function regenerateAttackChain() {
    if (!currentAttackChainConversationId) {
        return;
    }
    
    // Internal UI state handling.
    if (isAttackChainLoading(currentAttackChainConversationId)) {
        console.log('Generating attack chain, please wait...');
        return;
    }
    
    // Internal UI state handling.
    const savedConversationId = currentAttackChainConversationId;
    setAttackChainLoading(savedConversationId, true);
    
    const container = document.getElementById('attack-chain-container');
    if (container) {
        container.innerHTML = '<div class="loading-spinner">Generate...</div>';
    }
    
    // Internal UI state handling.
    const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
    if (regenerateBtn) {
        regenerateBtn.disabled = true;
        regenerateBtn.style.opacity = '0.5';
        regenerateBtn.style.cursor = 'not-allowed';
    }
    
    try {
        // Internal UI state handling.
        const response = await apiFetch(`/api/attack-chain/${savedConversationId}/regenerate`, {
            method: 'POST'
        });
        
        if (!response.ok) {
            // Internal UI state handling.
            if (response.status === 409) {
                const error = await response.json();
                if (container) {
                    container.innerHTML = `
                        <div class="loading-spinner" style="text-align: center; padding: 40px;">
                            <div style="margin-bottom: 16px;">⏳ Attack chainGenerate...</div>
                            <div style="color: var(--text-secondary); font-size: 0.875rem;">
                                Please wait; it will refresh automatically after generation completes.
                            </div>
                            <button class="btn-secondary" onclick="refreshAttackChain()" style="margin-top: 16px;">
                                Refresh
                            </button>
                        </div>
                    `;
                }
                // Internal UI state handling.
                // Internal UI state handling.
                setTimeout(() => {
                    // Internal UI state handling.
                    if (currentAttackChainConversationId === savedConversationId && 
                        isAttackChainLoading(savedConversationId)) {
                        refreshAttackChain();
                    }
                }, 5000);
                return;
            }
            
            const error = await response.json();
            throw new Error(error.error || 'Generate attack chainFailed');
        }
        
        const chainData = await response.json();
        
        // Internal UI state handling.
        if (currentAttackChainConversationId !== savedConversationId) {
            console.log('Attack chain, Chat, text', {
                returned: savedConversationId,
                current: currentAttackChainConversationId
            });
            setAttackChainLoading(savedConversationId, false);
            return;
        }
        
        // Internal UI state handling.
        renderAttackChain(chainData);
        
        // Internal UI state handling.
        updateAttackChainStats(chainData);
        
    } catch (error) {
        console.error('Generate attack chainFailed:', error);
        if (container) {
            container.innerHTML = `<div class="error-message">GenerateFailed: ${error.message}</div>`;
        }
    } finally {
        setAttackChainLoading(savedConversationId, false);
        
        // Internal UI state handling.
        if (regenerateBtn) {
            regenerateBtn.disabled = false;
            regenerateBtn.style.opacity = '1';
            regenerateBtn.style.cursor = 'pointer';
        }
    }
}

// Internal UI state handling.

// Internal UI state handling.
function _acEscapeXml(str) {
    if (str === null || str === undefined) return '';
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&apos;');
}

// Internal UI state handling.
function _acWrapLabel(label, maxChars, maxLines) {
    if (!label) return [''];
    const text = String(label).replace(/\s+/g, ' ').trim();
    if (!text) return [''];
    // Internal UI state handling.
    const width = (ch) => (/[\u4e00-\u9fa5\uff00-\uffef]/.test(ch) ? 2 : 1);
    const maxW = maxChars * 1.8; // Internal UI state handling.

    const lines = [];
    let buf = '';
    let bufW = 0;
    let lastSpaceIdx = -1;
    for (let i = 0; i < text.length; i++) {
        const ch = text[i];
        const w = width(ch);
        if (ch === ' ') lastSpaceIdx = buf.length;
        if (bufW + w > maxW) {
            // Internal UI state handling.
            let cut = buf;
            let rest = '';
            if (lastSpaceIdx > 0 && lastSpaceIdx >= buf.length - 10) {
                cut = buf.substring(0, lastSpaceIdx);
                rest = buf.substring(lastSpaceIdx + 1);
            }
            lines.push(cut);
            if (lines.length >= maxLines) {
                // Internal UI state handling.
                const last = lines[lines.length - 1];
                lines[lines.length - 1] = _acTruncateToWidth(last, maxW - 2) + '…';
                return lines;
            }
            buf = rest + ch;
            bufW = 0;
            for (let j = 0; j < buf.length; j++) bufW += width(buf[j]);
            lastSpaceIdx = -1;
        } else {
            buf += ch;
            bufW += w;
        }
    }
    if (buf) lines.push(buf);
    if (lines.length > maxLines) {
        const kept = lines.slice(0, maxLines);
        kept[kept.length - 1] = _acTruncateToWidth(kept[kept.length - 1], maxW - 2) + '…';
        return kept;
    }
    return lines;
}

function _acTruncateToWidth(str, maxW) {
    const width = (ch) => (/[\u4e00-\u9fa5\uff00-\uffef]/.test(ch) ? 2 : 1);
    let w = 0;
    let out = '';
    for (let i = 0; i < str.length; i++) {
        w += width(str[i]);
        if (w > maxW) break;
        out += str[i];
    }
    return out;
}

// Internal UI state handling.
function _acDarken(hex, amount) {
    try {
        const h = hex.replace('#', '');
        const r = parseInt(h.substring(0, 2), 16);
        const g = parseInt(h.substring(2, 4), 16);
        const b = parseInt(h.substring(4, 6), 16);
        const f = (c) => Math.max(0, Math.min(255, Math.round(c * (1 - amount))));
        return '#' + [f(r), f(g), f(b)].map(x => x.toString(16).padStart(2, '0')).join('');
    } catch (e) {
        return hex;
    }
}

// Internal UI state handling.
function _acCollectExportData() {
    if (!attackChainCytoscape) return null;
    const nodes = [];
    attackChainCytoscape.nodes().forEach(n => {
        // Internal UI state handling.
        if (n.style('display') === 'none') return;
        const pos = n.position();
        // Internal UI state handling.
        let w = n.outerWidth ? n.outerWidth() : n.width();
        let h = n.outerHeight ? n.outerHeight() : n.height();
        // Internal UI state handling.
        if (!w || !isFinite(w) || w < 40) w = 280;
        if (!h || !isFinite(h) || h < 30) h = 96;
        nodes.push({
            id: n.id(),
            x: pos.x,
            y: pos.y,
            w: w,
            h: h,
            type: n.data('type') || '',
            typeLabel: n.data('typeLabel') || '',
            typeBadge: n.data('typeBadge') || '•',
            typeColor: n.data('typeColor') || '#334155',
            accentColor: n.data('accentColor') || '#94a3b8',
            bgGradientStart: n.data('bgGradientStart') || '#FFFFFF',
            bgGradientEnd: n.data('bgGradientEnd') || '#F8FAFC',
            riskScore: n.data('riskScore') || 0,
            label: n.data('originalLabel') || n.data('label') || n.id(),
            metadata: n.data('metadata') || {}
        });
    });

    const edges = [];
    attackChainCytoscape.edges().forEach(e => {
        if (e.style('display') === 'none') return;
        const info = getEdgeNodes(e);
        if (!info.valid) return;
        const s = info.source.position();
        const t = info.target.position();
        edges.push({
            id: e.id(),
            source: info.source.id(),
            target: info.target.id(),
            sx: s.x, sy: s.y,
            tx: t.x, ty: t.y,
            type: e.data('type') || 'leads_to'
        });
    });

    return { nodes, edges };
}

// Internal UI state handling.
function _acGetNodeIconPath(type) {
    // Internal UI state handling.
    if (type === 'target') {
        // Internal UI state handling.
        return 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8zm0-14c-3.31 0-6 2.69-6 6s2.69 6 6 6 6-2.69 6-6-2.69-6-6-6zm0 10c-2.21 0-4-1.79-4-4s1.79-4 4-4 4 1.79 4 4-1.79 4-4 4z';
    }
    if (type === 'action') {
        // Internal UI state handling.
        return 'M7 2v11h3v9l7-12h-4l4-8z';
    }
    if (type === 'vulnerability') {
        // Internal UI state handling.
        return 'M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm-1 6h2v6h-2V7zm0 8h2v2h-2v-2z';
    }
    // Internal UI state handling.
    return 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z';
}

// Internal UI state handling.
function _acGetRiskLabel(score) {
    if (score >= 80) return 'Critical';
    if (score >= 60) return 'Copied';
    if (score >= 40) return 'Copied';
    if (score > 0) return 'Copied';
    return '';
}

// Internal UI state handling.
function _acBuildSvgString() {
    const data = _acCollectExportData();
    if (!data || data.nodes.length === 0) throw new Error('Copied');

    const { nodes, edges } = data;

    // Internal UI state handling.
    // Internal UI state handling.
    nodes.forEach(n => {
        // Internal UI state handling.
        // Internal UI state handling.
        n.w = 380;
        n.h = 140;
    });

    // Internal UI state handling.
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    nodes.forEach(n => {
        minX = Math.min(minX, n.x - n.w / 2);
        minY = Math.min(minY, n.y - n.h / 2);
        maxX = Math.max(maxX, n.x + n.w / 2);
        maxY = Math.max(maxY, n.y + n.h / 2);
    });

    // Internal UI state handling.
    const GRAPH_PAD = 100;                   // Internal UI state handling.
    const HEADER_H = 128;                    // Internal UI state handling.
    const FOOTER_H = 56;                     // Internal UI state handling.
    const LEGEND_W = 320;                    // Internal UI state handling.
    const OUTER_PAD = 32;                    // Internal UI state handling.

    const rawGraphW = (maxX - minX) + GRAPH_PAD * 2;
    const rawGraphH = (maxY - minY) + GRAPH_PAD * 2;

    const minGraphW = 900;
    const minGraphH = 620;
    const graphW = Math.max(rawGraphW, minGraphW);
    const graphH = Math.max(rawGraphH, minGraphH);

    const contentW = graphW + LEGEND_W + 36;
    const contentH = graphH + HEADER_H + FOOTER_H + 24;

    const totalW = contentW + OUTER_PAD * 2;
    const totalH = contentH + OUTER_PAD * 2;

    // Internal UI state handling.
    const graphAreaX = OUTER_PAD + 20;
    const graphAreaY = OUTER_PAD + HEADER_H;
    const graphAreaW = graphW - 4;
    const graphAreaH = contentH - HEADER_H - FOOTER_H;

    // Internal UI state handling.
    const graphCenterOffsetX = (graphAreaW - rawGraphW) / 2;
    const graphCenterOffsetY = (graphAreaH - rawGraphH) / 2;
    const graphOriginX = graphAreaX + GRAPH_PAD + graphCenterOffsetX - minX;
    const graphOriginY = graphAreaY + GRAPH_PAD + graphCenterOffsetY - minY;

    // Internal UI state handling.
    const legendX = graphAreaX + graphW + 16;

    // Internal UI state handling.
    const nodeCount = nodes.length;
    const edgeCount = edges.length;
    const vulnNodes = nodes.filter(n => n.type === 'vulnerability');
    const actionNodes = nodes.filter(n => n.type === 'action');
    const targetNodes = nodes.filter(n => n.type === 'target');
    const criticalCount = vulnNodes.filter(n => n.riskScore >= 80).length;
    const highCount = vulnNodes.filter(n => n.riskScore >= 60 && n.riskScore < 80).length;
    const medCount = vulnNodes.filter(n => n.riskScore >= 40 && n.riskScore < 60).length;
    const lowCount = vulnNodes.filter(n => n.riskScore > 0 && n.riskScore < 40).length;

    const timestamp = new Date();
    const ts = timestamp.getFullYear() + '-' +
        String(timestamp.getMonth() + 1).padStart(2, '0') + '-' +
        String(timestamp.getDate()).padStart(2, '0') + ' ' +
        String(timestamp.getHours()).padStart(2, '0') + ':' +
        String(timestamp.getMinutes()).padStart(2, '0');

    // Internal UI state handling.
    const typeTheme = {
        'target': { primary: '#4F46E5', light: '#EEF2FF', dark: '#3730A3', text: '#312E81', label: 'Target' },
        'action-success': { primary: '#10B981', light: '#ECFDF5', dark: '#047857', text: '#064E3B', label: 'Action with findings' },
        'action-neutral': { primary: '#64748B', light: '#F8FAFC', dark: '#475569', text: '#334155', label: 'Action' },
        'vuln-critical': { primary: '#E11D48', light: '#FFF1F2', dark: '#BE123C', text: '#881337', label: 'Critical' },
        'vuln-high': { primary: '#EA580C', light: '#FFF7ED', dark: '#C2410C', text: '#7C2D12', label: 'High vuln' },
        'vuln-med': { primary: '#CA8A04', light: '#FEFCE8', dark: '#A16207', text: '#713F12', label: 'Medium vuln' },
        'vuln-low': { primary: '#0D9488', light: '#F0FDFA', dark: '#0F766E', text: '#134E4A', label: 'Low vuln' }
    };

    function themeFor(n) {
        if (n.type === 'target') return typeTheme['target'];
        if (n.type === 'action') {
            const m = n.metadata || {};
            const findings = m.findings || [];
            const hasFindings = Array.isArray(findings) && findings.length > 0;
            const isFailed = m.status === 'failed_insight';
            return (hasFindings && !isFailed) ? typeTheme['action-success'] : typeTheme['action-neutral'];
        }
        if (n.type === 'vulnerability') {
            const s = n.riskScore || 0;
            if (s >= 80) return typeTheme['vuln-critical'];
            if (s >= 60) return typeTheme['vuln-high'];
            if (s >= 40) return typeTheme['vuln-med'];
            return typeTheme['vuln-low'];
        }
        return typeTheme['action-neutral'];
    }

    // Internal UI state handling.
    const nodesMap = new Map(nodes.map(n => [n.id, n]));

    // Internal UI state handling.
    const parts = [];
    parts.push(`<?xml version="1.0" encoding="UTF-8"?>`);
    parts.push(`<svg xmlns="http://www.w3.org/2000/svg" width="${totalW}" height="${totalH}" viewBox="0 0 ${totalW} ${totalH}" font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', 'Hiragino Sans GB', Roboto, Helvetica, Arial, sans-serif">`);

    // ==================== defs ====================
    parts.push(`<defs>`);

    // Internal UI state handling.
    parts.push(`<linearGradient id="ac-bg" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" stop-color="#FAFBFC"/>
        <stop offset="100%" stop-color="#F1F5F9"/>
    </linearGradient>`);

    // Internal UI state handling.
    parts.push(`<radialGradient id="ac-glow-1" cx="50%" cy="50%" r="50%">
        <stop offset="0%" stop-color="#6366F1" stop-opacity="0.12"/>
        <stop offset="100%" stop-color="#6366F1" stop-opacity="0"/>
    </radialGradient>`);
    parts.push(`<radialGradient id="ac-glow-2" cx="50%" cy="50%" r="50%">
        <stop offset="0%" stop-color="#EC4899" stop-opacity="0.08"/>
        <stop offset="100%" stop-color="#EC4899" stop-opacity="0"/>
    </radialGradient>`);
    parts.push(`<radialGradient id="ac-glow-3" cx="50%" cy="50%" r="50%">
        <stop offset="0%" stop-color="#06B6D4" stop-opacity="0.08"/>
        <stop offset="100%" stop-color="#06B6D4" stop-opacity="0"/>
    </radialGradient>`);

    // Internal UI state handling.
    parts.push(`<linearGradient id="ac-brand" x1="0%" y1="0%" x2="100%" y2="0%">
        <stop offset="0%" stop-color="#4F46E5"/>
        <stop offset="50%" stop-color="#7C3AED"/>
        <stop offset="100%" stop-color="#EC4899"/>
    </linearGradient>`);

    // Internal UI state handling.
    parts.push(`<pattern id="ac-dot" x="0" y="0" width="24" height="24" patternUnits="userSpaceOnUse">
        <circle cx="12" cy="12" r="1" fill="#0F172A" fill-opacity="0.06"/>
    </pattern>`);

    // Internal UI state handling.
    parts.push(`<filter id="ac-shadow-card" x="-20%" y="-20%" width="140%" height="140%">
        <feDropShadow dx="0" dy="1" stdDeviation="1.5" flood-color="#0F172A" flood-opacity="0.06"/>
        <feDropShadow dx="0" dy="6" stdDeviation="12" flood-color="#0F172A" flood-opacity="0.08"/>
    </filter>`);

    // Internal UI state handling.
    parts.push(`<filter id="ac-shadow-icon" x="-30%" y="-30%" width="160%" height="160%">
        <feDropShadow dx="0" dy="2" stdDeviation="3" flood-color="#0F172A" flood-opacity="0.15"/>
    </filter>`);

    // Internal UI state handling.
    parts.push(`<filter id="ac-shadow-badge" x="-30%" y="-30%" width="160%" height="160%">
        <feDropShadow dx="0" dy="1.5" stdDeviation="2.5" flood-color="#0F172A" flood-opacity="0.18"/>
    </filter>`);

    // Internal UI state handling.
    Object.keys(typeTheme).forEach(key => {
        const t = typeTheme[key];
        parts.push(`<linearGradient id="ac-icon-grad-${key}" x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" stop-color="${t.primary}"/>
            <stop offset="100%" stop-color="${t.dark}"/>
        </linearGradient>`);
    });

    // Internal UI state handling.
    edges.forEach((e, idx) => {
        const sNode = nodesMap.get(e.source);
        const tNode = nodesMap.get(e.target);
        if (!sNode || !tNode) return;
        const sTheme = themeFor(sNode);
        const tTheme = themeFor(tNode);
        parts.push(`<linearGradient id="ac-edge-grad-${idx}" gradientUnits="userSpaceOnUse" x1="${e.sx}" y1="${e.sy}" x2="${e.tx}" y2="${e.ty}">
            <stop offset="0%" stop-color="${sTheme.primary}" stop-opacity="0.7"/>
            <stop offset="100%" stop-color="${tTheme.primary}" stop-opacity="0.9"/>
        </linearGradient>`);
    });

    // Internal UI state handling.
    Object.keys(typeTheme).forEach(key => {
        const t = typeTheme[key];
        parts.push(`<marker id="ac-arrow-${key}" viewBox="0 0 12 12" refX="10" refY="6" markerWidth="8" markerHeight="8" orient="auto-start-reverse" markerUnits="strokeWidth">
            <path d="M 0 0 L 12 6 L 0 12 L 3 6 Z" fill="${t.primary}"/>
        </marker>`);
    });

    parts.push(`</defs>`);

    // Internal UI state handling.
    parts.push(`<rect x="0" y="0" width="${totalW}" height="${totalH}" fill="url(#ac-bg)"/>`);
    // Internal UI state handling.
    parts.push(`<ellipse cx="${totalW * 0.1}" cy="${totalH * 0.15}" rx="${totalW * 0.4}" ry="${totalH * 0.4}" fill="url(#ac-glow-1)"/>`);
    parts.push(`<ellipse cx="${totalW * 0.9}" cy="${totalH * 0.85}" rx="${totalW * 0.35}" ry="${totalH * 0.35}" fill="url(#ac-glow-2)"/>`);
    parts.push(`<ellipse cx="${totalW * 0.5}" cy="${totalH * 0.1}" rx="${totalW * 0.3}" ry="${totalH * 0.3}" fill="url(#ac-glow-3)"/>`);

    // Internal UI state handling.
    parts.push(`<rect x="${OUTER_PAD}" y="${OUTER_PAD}" width="${contentW}" height="${contentH}" rx="24" ry="24" fill="#FFFFFF" stroke="rgba(15,23,42,0.06)" stroke-width="1" filter="url(#ac-shadow-card)"/>`);

    // Internal UI state handling.
    const tX = OUTER_PAD + 40;
    const tY = OUTER_PAD + 28;

    // Internal UI state handling.
    parts.push(`<g filter="url(#ac-shadow-icon)">`);
    parts.push(`<rect x="${tX - 4}" y="${tY}" width="48" height="48" rx="12" fill="url(#ac-brand)"/>`);
    // Internal UI state handling.
    parts.push(`<g transform="translate(${tX - 4 + 12}, ${tY + 12}) scale(0.9)">
        <path d="M12 2L3 7v10l9 5 9-5V7z" fill="none" stroke="#FFFFFF" stroke-width="1.8" stroke-linejoin="round"/>
        <path d="M10 7l-2 5h3l-1 4 4-5h-3l1-4z" fill="#FFFFFF"/>
    </g>`);
    parts.push(`</g>`);

    // Internal UI state handling.
    parts.push(`<text x="${tX + 56}" y="${tY + 26}" font-size="26" font-weight="800" fill="#0F172A" letter-spacing="-0.6px">Attack chain</text>`);

    // Internal UI state handling.
    parts.push(`<text x="${tX + 56}" y="${tY + 50}" font-size="13" font-weight="500" fill="#64748B" letter-spacing="0.1px">Attack Chain Analysis · ${_acEscapeXml(ts)}</text>`);

    // Internal UI state handling.
    const kpiY = OUTER_PAD + 28;
    const kpiH = 48;
    const kpiGap = 12;
    const kpiW = 110;
    const kpiItems = [
        { label: 'Target', value: nodeCount, color: '#4F46E5' },
        { label: 'Target', value: edgeCount, color: '#06B6D4' },
        { label: 'Critical', value: criticalCount, color: criticalCount > 0 ? '#E11D48' : '#94A3B8' }
    ];
    let kpiXStart = OUTER_PAD + contentW - 40 - (kpiW * kpiItems.length + kpiGap * (kpiItems.length - 1));
    kpiItems.forEach((kpi, i) => {
        const kx = kpiXStart + i * (kpiW + kpiGap);
        // Internal UI state handling.
        parts.push(`<rect x="${kx}" y="${kpiY}" width="${kpiW}" height="${kpiH}" rx="12" fill="#FFFFFF" stroke="${kpi.color}" stroke-opacity="0.15" stroke-width="1"/>`);
        // Internal UI state handling.
        parts.push(`<rect x="${kx}" y="${kpiY + 10}" width="3" height="${kpiH - 20}" rx="1.5" fill="${kpi.color}"/>`);
        // Internal UI state handling.
        parts.push(`<text x="${kx + 16}" y="${kpiY + 26}" font-size="20" font-weight="800" fill="#0F172A" letter-spacing="-0.4px">${kpi.value}</text>`);
        // Internal UI state handling.
        parts.push(`<text x="${kx + 16}" y="${kpiY + 40}" font-size="10.5" font-weight="600" fill="#64748B" letter-spacing="0.4px">${_acEscapeXml(kpi.label)}</text>`);
    });

    // Internal UI state handling.
    parts.push(`<line x1="${OUTER_PAD + 40}" y1="${OUTER_PAD + HEADER_H - 10}" x2="${OUTER_PAD + contentW - 40}" y2="${OUTER_PAD + HEADER_H - 10}" stroke="rgba(15,23,42,0.08)" stroke-width="1"/>`);

    // Internal UI state handling.
    parts.push(`<rect x="${graphAreaX}" y="${graphAreaY}" width="${graphAreaW}" height="${graphAreaH}" rx="18" fill="#FCFCFD" stroke="rgba(15,23,42,0.05)" stroke-width="1"/>`);
    parts.push(`<rect x="${graphAreaX}" y="${graphAreaY}" width="${graphAreaW}" height="${graphAreaH}" rx="18" fill="url(#ac-dot)" opacity="0.7"/>`);

    // Internal UI state handling.
    parts.push(`<g transform="translate(${graphOriginX}, ${graphOriginY})">`);

    // Internal UI state handling.
    edges.forEach((e, idx) => {
        const sNode = nodesMap.get(e.source);
        const tNode = nodesMap.get(e.target);
        if (!sNode || !tNode) return;
        const tTheme = themeFor(tNode);

        const dx = e.tx - e.sx;
        const dy = e.ty - e.sy;
        const mag = Math.sqrt(dx * dx + dy * dy) || 1;
        const offset = Math.min(80, mag * 0.25);
        const nx = -dy / mag;
        const ny = dx / mag;
        const cx = (e.sx + e.tx) / 2 + nx * offset;
        const cy = (e.sy + e.ty) / 2 + ny * offset;
        const shrink = 22;
        const ex = e.tx - (dx / mag) * shrink;
        const ey = e.ty - (dy / mag) * shrink;

        const strokeWidth = (e.type === 'discovers' || e.type === 'enables') ? 2.4 : 2;
        const strokeDash = e.type === 'targets' ? 'stroke-dasharray="10,5"' : '';
        // Internal UI state handling.
        const targetThemeKey = Object.keys(typeTheme).find(k => typeTheme[k] === tTheme) || 'action-neutral';

        // Internal UI state handling.
        parts.push(`<path d="M ${e.sx.toFixed(1)} ${e.sy.toFixed(1)} Q ${cx.toFixed(1)} ${cy.toFixed(1)} ${ex.toFixed(1)} ${ey.toFixed(1)}" fill="none" stroke="${tTheme.primary}" stroke-width="${strokeWidth + 4}" stroke-linecap="round" stroke-opacity="0.08" ${strokeDash}/>`);
        // Internal UI state handling.
        parts.push(`<path d="M ${e.sx.toFixed(1)} ${e.sy.toFixed(1)} Q ${cx.toFixed(1)} ${cy.toFixed(1)} ${ex.toFixed(1)} ${ey.toFixed(1)}" fill="none" stroke="url(#ac-edge-grad-${idx})" stroke-width="${strokeWidth}" stroke-linecap="round" ${strokeDash} marker-end="url(#ac-arrow-${targetThemeKey})"/>`);
    });

    // Internal UI state handling.
    nodes.forEach((n, i) => {
        const theme = themeFor(n);
        const themeKey = Object.keys(typeTheme).find(k => typeTheme[k] === theme) || 'action-neutral';

        const x = n.x - n.w / 2;
        const y = n.y - n.h / 2;
        const r = 18;  // Internal UI state handling.

        // Internal UI state handling.
        // Internal UI state handling.
        parts.push(`<g filter="url(#ac-shadow-card)">`);
        parts.push(`<rect x="${x}" y="${y}" width="${n.w}" height="${n.h}" rx="${r}" fill="#FFFFFF"/>`);
        parts.push(`</g>`);
        // Internal UI state handling.
        parts.push(`<rect x="${x}" y="${y}" width="${n.w}" height="${n.h}" rx="${r}" fill="${theme.primary}" fill-opacity="0.02"/>`);
        // Internal UI state handling.
        parts.push(`<rect x="${x}" y="${y}" width="${n.w}" height="${n.h}" rx="${r}" fill="none" stroke="${theme.primary}" stroke-opacity="0.18" stroke-width="1"/>`);
        // Internal UI state handling.
        parts.push(`<rect x="${x + 20}" y="${y}" width="${n.w - 40}" height="3" rx="1.5" fill="${theme.primary}" fill-opacity="0.5"/>`);

        const padX = 24;
        const padY = 22;

        // Internal UI state handling.
        const iconSize = 44;
        const iconX = x + padX;
        const iconY = y + padY;

        // Internal UI state handling.
        parts.push(`<g filter="url(#ac-shadow-icon)">`);
        parts.push(`<rect x="${iconX}" y="${iconY}" width="${iconSize}" height="${iconSize}" rx="12" fill="url(#ac-icon-grad-${themeKey})"/>`);
        parts.push(`</g>`);
        // Internal UI state handling.
        const iconPath = _acGetNodeIconPath(n.type);
        const iconScale = (iconSize * 0.55) / 24;
        const iconInnerOffset = (iconSize - 24 * iconScale) / 2;
        parts.push(`<g transform="translate(${iconX + iconInnerOffset}, ${iconY + iconInnerOffset}) scale(${iconScale.toFixed(3)})">
            <path d="${iconPath}" fill="#FFFFFF"/>
        </g>`);

        // Internal UI state handling.
        const typeTextX = iconX + iconSize + 14;
        // Internal UI state handling.
        const typeEn = n.type === 'target' ? 'TARGET' : n.type === 'action' ? 'ACTION' : n.type === 'vulnerability' ? 'VULNERABILITY' : (n.type || '').toUpperCase();
        parts.push(`<text x="${typeTextX}" y="${iconY + 14}" font-size="10" font-weight="700" fill="${theme.dark}" fill-opacity="0.75" letter-spacing="1.2px">${_acEscapeXml(typeEn)}</text>`);
        // Internal UI state handling.
        parts.push(`<text x="${typeTextX}" y="${iconY + 34}" font-size="16" font-weight="700" fill="${theme.text}" letter-spacing="-0.2px">${_acEscapeXml(theme.label)}</text>`);

        // Internal UI state handling.
        const badgeY = iconY + 2;
        const badgeH = 26;
        if (n.type === 'vulnerability' && n.riskScore > 0) {
            // Internal UI state handling.
            const riskLabel = _acGetRiskLabel(n.riskScore);
            const badgeText = `${riskLabel} · ${n.riskScore}`;
            const badgeW = 90;
            const bx = x + n.w - badgeW - padX;
            parts.push(`<g filter="url(#ac-shadow-badge)">`);
            parts.push(`<rect x="${bx}" y="${badgeY}" width="${badgeW}" height="${badgeH}" rx="${badgeH / 2}" fill="url(#ac-icon-grad-${themeKey})"/>`);
            parts.push(`<text x="${bx + badgeW / 2}" y="${badgeY + badgeH / 2 + 4.5}" text-anchor="middle" font-size="12" font-weight="700" fill="#FFFFFF" letter-spacing="0.2px">${_acEscapeXml(badgeText)}</text>`);
            parts.push(`</g>`);
        } else if (n.type === 'action') {
            const m = n.metadata || {};
            const findings = m.findings || [];
            const hasFindings = Array.isArray(findings) && findings.length > 0;
            const isFailed = m.status === 'failed_insight';
            if (hasFindings || isFailed) {
                const text = isFailed ? 'Copied' : `text ${findings.length}`;
                const badgeW = 70;
                const bx = x + n.w - badgeW - padX;
                parts.push(`<rect x="${bx}" y="${badgeY}" width="${badgeW}" height="${badgeH}" rx="${badgeH / 2}" fill="${theme.primary}" fill-opacity="0.12" stroke="${theme.primary}" stroke-opacity="0.4" stroke-width="1"/>`);
                // Internal UI state handling.
                parts.push(`<circle cx="${bx + 12}" cy="${badgeY + badgeH / 2}" r="3" fill="${theme.primary}"/>`);
                parts.push(`<text x="${bx + 20}" y="${badgeY + badgeH / 2 + 4.5}" font-size="11.5" font-weight="700" fill="${theme.dark}">${_acEscapeXml(text)}</text>`);
            }
        } else if (n.type === 'target') {
            // Internal UI state handling.
            const badgeW = 60;
            const bx = x + n.w - badgeW - padX;
            parts.push(`<rect x="${bx}" y="${badgeY}" width="${badgeW}" height="${badgeH}" rx="${badgeH / 2}" fill="${theme.primary}" fill-opacity="0.12" stroke="${theme.primary}" stroke-opacity="0.4" stroke-width="1"/>`);
            parts.push(`<text x="${bx + badgeW / 2}" y="${badgeY + badgeH / 2 + 4.5}" text-anchor="middle" font-size="11.5" font-weight="700" fill="${theme.dark}" letter-spacing="0.3px">text</text>`);
        }

        // Internal UI state handling.
        const contentTopY = iconY + iconSize + 18;
        const titleFontSize = 16;
        const titleLineH = titleFontSize + 6;
        const contentAvailW = n.w - padX * 2;
        const charsPerLine = Math.max(10, Math.floor(contentAvailW / (titleFontSize * 0.58)));
        const titleLines = _acWrapLabel(n.label, charsPerLine, 2);
        titleLines.forEach((ln, idx) => {
            parts.push(`<text x="${x + padX}" y="${contentTopY + idx * titleLineH}" font-size="${titleFontSize}" font-weight="700" fill="#0F172A" letter-spacing="-0.2px">${_acEscapeXml(ln)}</text>`);
        });

        // Internal UI state handling.
        const metaY = y + n.h - 22;
        // Internal UI state handling.
        parts.push(`<line x1="${x + padX}" y1="${metaY - 10}" x2="${x + n.w - padX}" y2="${metaY - 10}" stroke="rgba(15,23,42,0.06)" stroke-width="1"/>`);

        // Internal UI state handling.
        const metaItems = [];
        if (n.type === 'target') {
            const tgt = (n.metadata && n.metadata.target) ? n.metadata.target : null;
            if (tgt) metaItems.push({ icon: 'loc', text: _acTruncateToWidth(tgt, 26) });
        } else if (n.type === 'action') {
            const toolName = n.metadata && n.metadata.tool_name;
            if (toolName) metaItems.push({ icon: 'tool', text: _acTruncateToWidth(toolName, 20) });
            const intent = n.metadata && n.metadata.tool_intent;
            if (intent) metaItems.push({ icon: 'aim', text: _acTruncateToWidth(intent, 22) });
        } else if (n.type === 'vulnerability') {
            const vt = n.metadata && n.metadata.vulnerability_type;
            if (vt) metaItems.push({ icon: 'shield', text: _acTruncateToWidth(vt, 22) });
            const sev = n.metadata && n.metadata.severity;
            if (sev) metaItems.push({ icon: 'alert', text: _acTruncateToWidth(sev, 12) });
        }
        if (metaItems.length === 0) {
            // Internal UI state handling.
            metaItems.push({ icon: 'hash', text: _acTruncateToWidth(n.id || '', 20) });
        }

        // Internal UI state handling.
        const metaIconPaths = {
            'loc': 'M12 2C8.13 2 5 5.13 5 9c0 5.25 7 13 7 13s7-7.75 7-13c0-3.87-3.13-7-7-7zm0 9.5a2.5 2.5 0 1 1 0-5 2.5 2.5 0 0 1 0 5z',
            'tool': 'M22.7 19l-9.1-9.1c.9-2.3.4-5-1.5-6.9-2-2-5-2.4-7.4-1.3L9 6 6 9 1.6 4.7C.4 7.1.9 10.1 2.9 12.1c1.9 1.9 4.6 2.4 6.9 1.5l9.1 9.1c.4.4 1 .4 1.4 0l2.3-2.3c.5-.4.5-1.1.1-1.4z',
            'aim': 'M12 2L4 5v6c0 5.5 3.8 10.7 8 12 4.2-1.3 8-6.5 8-12V5l-8-3zm4 10H8V9h3V7l3 3-3 3v-1z',
            'shield': 'M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm-2 16l-4-4 1.4-1.4 2.6 2.6 6.6-6.6L18 9l-8 8z',
            'alert': 'M1 21h22L12 2 1 21zm12-3h-2v-2h2v2zm0-4h-2v-4h2v4z',
            'hash': 'M20 9h-4.5l.9-4h-2l-.9 4H9l.9-4H8l-.9 4H3v2h3.7l-1 4H2v2h3.3l-.9 4h2l.9-4H12l-.9 4h2l.9-4H19v-2h-4.7l1-4H20V9zm-6.3 6H9l1-4h4.7l-1 4z'
        };

        let metaX = x + padX;
        metaItems.forEach((mi, idx) => {
            if (idx > 0) {
                // Internal UI state handling.
                parts.push(`<circle cx="${metaX + 6}" cy="${metaY}" r="1.2" fill="#CBD5E1"/>`);
                metaX += 14;
            }
            // Internal UI state handling.
            const path = metaIconPaths[mi.icon] || metaIconPaths.hash;
            parts.push(`<g transform="translate(${metaX}, ${metaY - 7}) scale(${(13 / 24).toFixed(3)})">
                <path d="${path}" fill="${theme.primary}" fill-opacity="0.8"/>
            </g>`);
            metaX += 18;
            // Internal UI state handling.
            parts.push(`<text x="${metaX}" y="${metaY + 3}" font-size="11.5" font-weight="500" fill="#64748B">${_acEscapeXml(mi.text)}</text>`);
            metaX += mi.text.length * 6.5;  // Internal UI state handling.
        });
    });

    parts.push(`</g>`);

    // Internal UI state handling.
    const lx = legendX;
    const ly = graphAreaY;
    const lw = LEGEND_W - 16;
    const lh = graphAreaH;

    // Internal UI state handling.
    parts.push(`<rect x="${lx}" y="${ly}" width="${lw}" height="${lh}" rx="18" fill="#FFFFFF" stroke="rgba(15,23,42,0.06)" stroke-width="1"/>`);
    // Internal UI state handling.
    parts.push(`<rect x="${lx + 16}" y="${ly}" width="${lw - 32}" height="3" rx="1.5" fill="url(#ac-brand)"/>`);

    let curY = ly + 26;

    // Internal UI state handling.
    parts.push(`<text x="${lx + 24}" y="${curY}" font-size="10.5" font-weight="800" fill="#64748B" letter-spacing="1.5px">NODE TYPES · Type</text>`);
    curY += 22;
    const typeSummary = [
        { key: 'target', count: targetNodes.length, text: 'Target' },
        { key: 'action-success', count: actionNodes.filter(a => { const m = a.metadata || {}; return Array.isArray(m.findings) && m.findings.length > 0 && m.status !== 'failed_insight'; }).length, text: 'Action (with findings)' },
        { key: 'action-neutral', count: actionNodes.filter(a => { const m = a.metadata || {}; const f = Array.isArray(m.findings) ? m.findings : []; return f.length === 0 || m.status === 'failed_insight'; }).length, text: 'Action (other)' },
        { key: 'vuln-critical', count: criticalCount, text: 'Critical' },
        { key: 'vuln-high', count: highCount, text: 'High risk' },
        { key: 'vuln-med', count: medCount, text: 'Medium risk' },
        { key: 'vuln-low', count: lowCount, text: 'Low risk' }
    ];
    typeSummary.forEach(item => {
        const t = typeTheme[item.key];
        if (item.count === 0) return;  // Internal UI state handling.
        // Internal UI state handling.
        parts.push(`<rect x="${lx + 24}" y="${curY - 10}" width="14" height="14" rx="4" fill="${t.primary}"/>`);
        // Internal UI state handling.
        parts.push(`<text x="${lx + 46}" y="${curY + 1}" font-size="12.5" font-weight="500" fill="#334155">${_acEscapeXml(item.text)}</text>`);
        // Internal UI state handling.
        parts.push(`<text x="${lx + lw - 24}" y="${curY + 1}" font-size="12.5" font-weight="700" fill="#0F172A" text-anchor="end">${item.count}</text>`);
        curY += 22;
    });
    curY += 10;

    // Internal UI state handling.
    parts.push(`<text x="${lx + 24}" y="${curY}" font-size="10.5" font-weight="800" fill="#64748B" letter-spacing="1.5px">CONNECTIONS · Meaning</text>`);
    curY += 22;
    const lineItems = [
        { label: 'Target', color: '#4F46E5', dash: '' },
        { label: 'Action discovers vulnerability', color: '#E11D48', dash: '' },
        { label: 'Target', color: '#64748B', dash: '' },
        { label: 'Target', color: '#4F46E5', dash: '6,3' }
    ];
    lineItems.forEach(l => {
        const dashAttr = l.dash ? `stroke-dasharray="${l.dash}"` : '';
        parts.push(`<line x1="${lx + 24}" y1="${curY - 3}" x2="${lx + 62}" y2="${curY - 3}" stroke="${l.color}" stroke-width="2.4" stroke-linecap="round" ${dashAttr}/>`);
        parts.push(`<polygon points="${lx + 62},${curY - 6} ${lx + 68},${curY - 3} ${lx + 62},${curY}" fill="${l.color}"/>`);
        parts.push(`<text x="${lx + 78}" y="${curY + 1}" font-size="12.5" font-weight="500" fill="#334155">${_acEscapeXml(l.label)}</text>`);
        curY += 24;
    });
    curY += 10;

    // Internal UI state handling.
    parts.push(`<text x="${lx + 24}" y="${curY}" font-size="10.5" font-weight="800" fill="#64748B" letter-spacing="1.5px">RISK LEVELS · Risk level</text>`);
    curY += 22;
    const riskBar = [
        { label: 'Critical', range: '80-100', color: '#E11D48' },
        { label: 'Target', range: '60-79', color: '#EA580C' },
        { label: 'Target', range: '40-59', color: '#CA8A04' },
        { label: 'Target', range: '0-39', color: '#0D9488' }
    ];
    riskBar.forEach(r => {
        // Internal UI state handling.
        parts.push(`<rect x="${lx + 24}" y="${curY - 10}" width="46" height="18" rx="9" fill="${r.color}"/>`);
        parts.push(`<text x="${lx + 47}" y="${curY + 2}" text-anchor="middle" font-size="10.5" font-weight="700" fill="#FFFFFF" letter-spacing="0.3px">${_acEscapeXml(r.label)}</text>`);
        // Internal UI state handling.
        parts.push(`<text x="${lx + 80}" y="${curY + 1}" font-size="12" font-weight="500" fill="#64748B">score ${_acEscapeXml(r.range)}</text>`);
        curY += 26;
    });

    // Internal UI state handling.
    const fY = OUTER_PAD + contentH - FOOTER_H;
    // Internal UI state handling.
    parts.push(`<line x1="${OUTER_PAD + 40}" y1="${fY + 16}" x2="${OUTER_PAD + contentW - 40}" y2="${fY + 16}" stroke="rgba(15,23,42,0.06)" stroke-width="1"/>`);
    // Internal UI state handling.
    parts.push(`<circle cx="${OUTER_PAD + 44}" cy="${fY + 34}" r="5" fill="url(#ac-brand)"/>`);
    parts.push(`<text x="${OUTER_PAD + 56}" y="${fY + 38}" font-size="11.5" font-weight="600" fill="#64748B">CyberStrikeAI <tspan fill="#94A3B8" font-weight="500">· Attack Chain Visualization Report</tspan></text>`);
    // Internal UI state handling.
    parts.push(`<text x="${OUTER_PAD + contentW - 40}" y="${fY + 38}" font-size="11.5" font-weight="500" fill="#94A3B8" text-anchor="end">${_acEscapeXml(ts)}</text>`);

    parts.push(`</svg>`);
    return parts.join('\n');
}

// Internal UI state handling.
function _acDownloadBlob(blob, filename) {
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    setTimeout(() => URL.revokeObjectURL(url), 150);
}

// Internal UI state handling.
function _acSvgToPng(svgString, scale) {
    return new Promise((resolve, reject) => {
        try {
            // Internal UI state handling.
            const m = svgString.match(/<svg[^>]*width="(\d+(?:\.\d+)?)"[^>]*height="(\d+(?:\.\d+)?)"/i);
            const w = m ? parseFloat(m[1]) : 1600;
            const h = m ? parseFloat(m[2]) : 900;
            const s = scale || Math.min(2.5, Math.max(1.5, 2000 / Math.max(w, h)));

            const blob = new Blob([svgString], { type: 'image/svg+xml;charset=utf-8' });
            const url = URL.createObjectURL(blob);
            const img = new Image();
            img.onload = function () {
                try {
                    const canvas = document.createElement('canvas');
                    canvas.width = Math.round(w * s);
                    canvas.height = Math.round(h * s);
                    const ctx = canvas.getContext('2d');
                    ctx.imageSmoothingEnabled = true;
                    ctx.imageSmoothingQuality = 'high';
                    ctx.fillStyle = '#ffffff';
                    ctx.fillRect(0, 0, canvas.width, canvas.height);
                    ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
                    URL.revokeObjectURL(url);
                    canvas.toBlob(pngBlob => {
                        if (!pngBlob) reject(new Error('PNG GenerateFailed'));
                        else resolve(pngBlob);
                    }, 'image/png', 0.95);
                } catch (err) {
                    URL.revokeObjectURL(url);
                    reject(err);
                }
            };
            img.onerror = function (e) {
                URL.revokeObjectURL(url);
                reject(new Error('SVG Failed'));
            };
            img.src = url;
        } catch (e) {
            reject(e);
        }
    });
}

// Internal UI state handling.
function exportAttackChain(format) {
    if (!attackChainCytoscape) {
        alert(typeof window.t === 'function' ? window.t('chat.pleaseLoadAttackChainFirst', {}, 'Attack chain') : 'Attack chain');
        return;
    }

    // Internal UI state handling.
    setTimeout(() => {
        try {
            const svgString = _acBuildSvgString();
            const convId = currentAttackChainConversationId || 'export';
            const tsName = Date.now();

            if (format === 'svg') {
                const blob = new Blob([svgString], { type: 'image/svg+xml;charset=utf-8' });
                _acDownloadBlob(blob, `attack-chain-${convId}-${tsName}.svg`);
            } else if (format === 'png') {
                _acSvgToPng(svgString, 2)
                    .then(pngBlob => _acDownloadBlob(pngBlob, `attack-chain-${convId}-${tsName}.png`))
                    .catch(err => {
                        console.error('PNG export failed, falling back to Cytoscape export:', err);
                        // Internal UI state handling.
                        try {
                            const p = attackChainCytoscape.png({ output: 'blob', bg: '#ffffff', full: true, scale: 2 });
                            if (p && typeof p.then === 'function') {
                                p.then(b => _acDownloadBlob(b, `attack-chain-${convId}-${tsName}.png`))
                                    .catch(e => alert('PNG export failed: ' + (e && e.message || e)));
                            } else if (p) {
                                _acDownloadBlob(p, `attack-chain-${convId}-${tsName}.png`);
                            } else {
                                alert('PNG export failed');
                            }
                        } catch (e2) {
                            alert('PNG export failed: ' + (e2 && e2.message || e2));
                        }
                    });
            } else {
                alert('Unsupported export format: ' + format);
            }
        } catch (error) {
            console.error('Failed:', error);
            alert('Failed: ' + (error && error.message || 'UnknownError'));
        }
    }, 80);
}

// ============================================
// Internal UI state handling.
// ============================================

// Internal UI state handling.
let currentGroupId = null; // grouppage
let currentConversationGroupId = null; // Internal UI state handling.
let contextMenuConversationId = null;
let contextMenuGroupId = null;
let groupsCache = [];
let conversationGroupMappingCache = {};
let pendingGroupMappings = {}; // Internal UI state handling.
let conversationsListLoadSeq = 0; // Internal UI state handling.

// Internal UI state handling.
async function loadGroups() {
    try {
        const response = await apiFetch('/api/groups');
        if (!response.ok) {
            groupsCache = [];
            return;
        }
        const data = await response.json();
        // Internal UI state handling.
        if (Array.isArray(data)) {
            groupsCache = data;
        } else {
            // Internal UI state handling.
            groupsCache = [];
        }

        const groupsList = document.getElementById('conversation-groups-list');
        if (!groupsList) return;

        groupsList.innerHTML = '';

        if (!Array.isArray(groupsCache) || groupsCache.length === 0) {
            return;
        }

        // Internal UI state handling.
        const sortedGroups = [...groupsCache];

            sortedGroups.forEach(group => {
            const groupItem = document.createElement('div');
            groupItem.className = 'group-item';
            // Internal UI state handling.
            // Internal UI state handling.
            // Internal UI state handling.
            const shouldHighlight = currentGroupId 
                ? (currentGroupId === group.id)
                : (currentConversationGroupId === group.id);
            if (shouldHighlight) {
                groupItem.classList.add('active');
            }
            const isPinned = group.pinned || false;
            if (isPinned) {
                groupItem.classList.add('pinned');
            }
            groupItem.dataset.groupId = group.id;

            const content = document.createElement('div');
            content.className = 'group-item-content';

            const icon = document.createElement('span');
            icon.className = 'group-item-icon';
            icon.textContent = group.icon || '📁';

            const name = document.createElement('span');
            name.className = 'group-item-name';
            name.textContent = group.name;

            content.appendChild(icon);
            content.appendChild(name);

            // Internal UI state handling.
            if (isPinned) {
                const pinIcon = document.createElement('span');
                pinIcon.className = 'group-item-pinned';
                pinIcon.innerHTML = '📌';
                pinIcon.title = 'Copied';
                name.appendChild(pinIcon);
            }
            groupItem.appendChild(content);

            const menuBtn = document.createElement('button');
            menuBtn.className = 'group-item-menu';
            menuBtn.innerHTML = '⋯';
            menuBtn.onclick = (e) => {
                e.stopPropagation();
                showGroupContextMenu(e, group.id);
            };
            groupItem.appendChild(menuBtn);

            groupItem.onclick = () => {
                enterGroupDetail(group.id);
            };

            groupsList.appendChild(groupItem);
        });
    } catch (error) {
        console.error('Risk scoreFailed:', error);
    }
}

// Internal UI state handling.
async function loadConversationsWithGroups(searchQuery = '') {
    const loadSeq = ++conversationsListLoadSeq;
    try {
        // Internal UI state handling.
        const limit = (searchQuery && searchQuery.trim()) ? 100 : 100;
        let url = `/api/conversations?limit=${limit}`;
        if (searchQuery && searchQuery.trim()) {
            url += '&search=' + encodeURIComponent(searchQuery.trim());
        }
        const [,, response] = await Promise.all([
            loadGroups(),
            loadConversationGroupMapping(),
            apiFetch(url),
        ]);
        if (loadSeq !== conversationsListLoadSeq) return;

        const listContainer = document.getElementById('conversations-list');
        if (!listContainer) {
            return;
        }

        // Internal UI state handling.
        const sidebarContent = listContainer.closest('.sidebar-content');
        const savedScrollTop = sidebarContent ? sidebarContent.scrollTop : 0;

        const emptyStateHtml = '<div style="padding: 20px; text-align: center; color: var(--text-muted); font-size: 0.875rem;" data-i18n="chat.noHistoryConversations"></div>';
        listContainer.innerHTML = '';

        // Internal UI state handling.
        if (!response.ok) {
            listContainer.innerHTML = emptyStateHtml;
            if (typeof window.applyTranslations === 'function') window.applyTranslations(listContainer);
            return;
        }

        const conversations = await response.json();
        if (loadSeq !== conversationsListLoadSeq) return;

        // Internal UI state handling.
        const uniqueConversations = [];
        const seenConversationIds = new Set();
        (Array.isArray(conversations) ? conversations : []).forEach(conv => {
            if (!conv || !conv.id || seenConversationIds.has(conv.id)) {
                return;
            }
            seenConversationIds.add(conv.id);
            uniqueConversations.push(conv);
        });

        if (uniqueConversations.length === 0) {
            listContainer.innerHTML = emptyStateHtml;
            if (typeof window.applyTranslations === 'function') window.applyTranslations(listContainer);
            return;
        }
        
        // Internal UI state handling.
        const pinnedConvs = [];
        const normalConvs = [];
        const hasSearchQuery = searchQuery && searchQuery.trim();

        uniqueConversations.forEach(conv => {
            // Internal UI state handling.
            if (hasSearchQuery) {
                // Internal UI state handling.
                if (conv.pinned) {
                    pinnedConvs.push(conv);
                } else {
                    normalConvs.push(conv);
                }
                return;
            }

            // Internal UI state handling.
            // Internal UI state handling.
            // Internal UI state handling.
            if (conversationGroupMappingCache[conv.id]) {
                // Internal UI state handling.
                return;
            }

            if (conv.pinned) {
                pinnedConvs.push(conv);
            } else {
                normalConvs.push(conv);
            }
        });

        // Internal UI state handling.
        const sortByTime = (a, b) => {
            const timeA = a.updatedAt ? new Date(a.updatedAt) : new Date(0);
            const timeB = b.updatedAt ? new Date(b.updatedAt) : new Date(0);
            return timeB - timeA;
        };

        pinnedConvs.sort(sortByTime);
        normalConvs.sort(sortByTime);

        const now = new Date();
        const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate());
        const yesterdayStart = new Date(todayStart);
        yesterdayStart.setDate(todayStart.getDate() - 1);
        const sevenDaysCutoff = new Date(todayStart);
        sevenDaysCutoff.setDate(todayStart.getDate() - 7);

        const tFn = typeof window.t === 'function' ? window.t.bind(window) : null;
        const groupOrder = [
            { key: 'today', label: tFn ? tFn('chat.historyGroupToday') : 'Copied' },
            { key: 'yesterday', label: tFn ? tFn('chat.yesterday') : 'Copied' },
            { key: 'last7Days', label: tFn ? tFn('chat.historyGroupLast7Days') : 'Copied' },
            { key: 'earlier', label: tFn ? tFn('chat.historyGroupEarlier') : 'Copied' },
        ];

        const groups = {
            today: [],
            yesterday: [],
            last7Days: [],
            earlier: [],
        };

        normalConvs.forEach(conv => {
            const dateObj = conv.updatedAt ? new Date(conv.updatedAt) : new Date();
            const validDate = isNaN(dateObj.getTime()) ? new Date() : dateObj;
            const groupKey = getConversationGroup(validDate, todayStart, sevenDaysCutoff, yesterdayStart);
            groups[groupKey].push({
                ...conv,
                _timeText: formatConversationTimestamp(validDate, todayStart, yesterdayStart),
            });
        });

        const fragment = document.createDocumentFragment();

        if (pinnedConvs.length > 0) {
            pinnedConvs.forEach(conv => {
                const dateObj = conv.updatedAt ? new Date(conv.updatedAt) : new Date();
                const validDate = isNaN(dateObj.getTime()) ? new Date() : dateObj;
                fragment.appendChild(createConversationListItemWithMenu({
                    ...conv,
                    _timeText: formatConversationTimestamp(validDate, todayStart, yesterdayStart),
                }, true));
            });
        }

        groupOrder.forEach(({ key, label }) => {
            const items = groups[key];
            if (!items || items.length === 0) {
                return;
            }
            const section = document.createElement('div');
            section.className = 'conversation-group';

            const title = document.createElement('div');
            title.className = 'conversation-group-title';
            title.textContent = label;
            section.appendChild(title);

            items.forEach(itemData => {
                section.appendChild(createConversationListItemWithMenu(itemData, false));
            });

            fragment.appendChild(section);
        });

        if (fragment.children.length === 0) {
            listContainer.innerHTML = emptyStateHtml;
            if (typeof window.applyTranslations === 'function') window.applyTranslations(listContainer);
            return;
        }

        if (loadSeq !== conversationsListLoadSeq) return;
        listContainer.appendChild(fragment);
        updateActiveConversation();
        
        // Internal UI state handling.
        if (sidebarContent) {
            // Internal UI state handling.
            requestAnimationFrame(() => {
                if (loadSeq === conversationsListLoadSeq) {
                    sidebarContent.scrollTop = savedScrollTop;
                }
            });
        }
    } catch (error) {
        if (loadSeq !== conversationsListLoadSeq) return;
        console.error('ChatFailed:', error);
        // Internal UI state handling.
        const listContainer = document.getElementById('conversations-list');
        if (listContainer) {
            const emptyStateHtml = '<div style="padding: 20px; text-align: center; color: var(--text-muted); font-size: 0.875rem;" data-i18n="chat.noHistoryConversations"></div>';
            listContainer.innerHTML = emptyStateHtml;
            if (typeof window.applyTranslations === 'function') window.applyTranslations(listContainer);
        }
    }
}

// Internal UI state handling.
function createConversationListItemWithMenu(conversation, isPinned) {
    const item = document.createElement('div');
    item.className = 'conversation-item';
    item.dataset.conversationId = conversation.id;
    if (conversation.id === currentConversationId) {
        item.classList.add('active');
    }

    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'conversation-content';

    const titleWrapper = document.createElement('div');
    titleWrapper.style.display = 'flex';
    titleWrapper.style.alignItems = 'center';
    titleWrapper.style.gap = '4px';

    const title = document.createElement('div');
    title.className = 'conversation-title';
    const titleText = conversation.title || 'Chat';
    title.textContent = safeTruncateText(titleText, 60);
    title.title = titleText; // Internal UI state handling.
    titleWrapper.appendChild(title);

    if (isPinned) {
        const pinIcon = document.createElement('span');
        pinIcon.className = 'conversation-item-pinned';
        pinIcon.innerHTML = '📌';
        pinIcon.title = 'Copied';
        titleWrapper.appendChild(pinIcon);
    }

    contentWrapper.appendChild(titleWrapper);

    const time = document.createElement('div');
    time.className = 'conversation-time';
    const dateObj = conversation.updatedAt ? new Date(conversation.updatedAt) : new Date();
    time.textContent = conversation._timeText || formatConversationTimestamp(dateObj);
    contentWrapper.appendChild(time);

    // Internal UI state handling.
    const groupId = conversationGroupMappingCache[conversation.id];
    if (groupId) {
        const group = groupsCache.find(g => g.id === groupId);
        if (group) {
            const groupTag = document.createElement('div');
            groupTag.className = 'conversation-group-tag';
            groupTag.innerHTML = `<span class="group-tag-icon">${group.icon || '📁'}</span><span class="group-tag-name">${group.name}</span>`;
            groupTag.title = `group: ${group.name}`;
            contentWrapper.appendChild(groupTag);
        }
    }

    item.appendChild(contentWrapper);

    const menuBtn = document.createElement('button');
    menuBtn.className = 'conversation-item-menu';
    menuBtn.innerHTML = '⋯';
    menuBtn.onclick = (e) => {
        e.stopPropagation();
        contextMenuConversationId = conversation.id;
        showConversationContextMenu(e);
    };
    item.appendChild(menuBtn);

    item.onclick = (e) => {
        e.preventDefault();
        e.stopPropagation();
        if (currentGroupId) {
            exitGroupDetail();
        }
        loadConversation(conversation.id);
    };

    return item;
}

// Internal UI state handling.
async function showConversationContextMenu(event) {
    const menu = document.getElementById('conversation-context-menu');
    if (!menu) return;

    // Internal UI state handling.
    const submenu = document.getElementById('move-to-group-submenu');
    if (submenu) {
        submenu.style.display = 'none';
        submenuVisible = false;
    }
    const downloadSubmenu = document.getElementById('download-markdown-submenu');
    if (downloadSubmenu) {
        downloadSubmenu.style.display = 'none';
    }
    // Internal UI state handling.
    clearSubmenuHideTimeout();
    clearSubmenuShowTimeout();
    clearDownloadMarkdownSubmenuHideTimeout();
    submenuLoading = false;

    const convId = contextMenuConversationId;
    
    // Internal UI state handling.
    const attackChainMenuItem = document.getElementById('attack-chain-menu-item');
    if (attackChainMenuItem) {
        if (convId) {
            const isRunning = typeof isConversationTaskRunning === 'function'
                ? isConversationTaskRunning(convId)
                : false;
            if (isRunning) {
                attackChainMenuItem.style.opacity = '0.5';
                attackChainMenuItem.style.cursor = 'not-allowed';
                attackChainMenuItem.onclick = null;
                attackChainMenuItem.title = 'ChatExecute, Generate attack chain';
            } else {
                attackChainMenuItem.style.opacity = '1';
                attackChainMenuItem.style.cursor = 'pointer';
                attackChainMenuItem.onclick = showAttackChainFromContext;
                attackChainMenuItem.title = (typeof window.t === 'function' ? window.t('chat.viewAttackChainCurrentConv') : 'ChatAttack chain');
            }
        } else {
            attackChainMenuItem.style.opacity = '0.5';
            attackChainMenuItem.style.cursor = 'not-allowed';
            attackChainMenuItem.onclick = null;
            attackChainMenuItem.title = (typeof window.t === 'function' ? window.t('chat.viewAttackChainSelectConv') : 'conversationAttack chain');
        }
    }
    
    // Internal UI state handling.
    if (convId) {
        try {
            let isPinned = false;
            // Internal UI state handling.
            const conversationGroupId = conversationGroupMappingCache[convId];
            const isInCurrentGroup = currentGroupId && conversationGroupId === currentGroupId;
            
            if (isInCurrentGroup) {
                // Internal UI state handling.
                const response = await apiFetch(`/api/groups/${currentGroupId}/conversations`);
                if (response.ok) {
                    const groupConvs = await response.json();
                    const conv = groupConvs.find(c => c.id === convId);
                    if (conv) {
                        isPinned = conv.groupPinned || false;
                    }
                }
            } else {
                // Internal UI state handling.
                const response = await apiFetch(`/api/conversations/${convId}`);
                if (response.ok) {
                    const conv = await response.json();
                    isPinned = conv.pinned || false;
                }
            }
            
            // Internal UI state handling.
            const pinMenuText = document.getElementById('pin-conversation-menu-text');
            if (pinMenuText && typeof window.t === 'function') {
                pinMenuText.textContent = isPinned ? window.t('contextMenu.unpinConversation') : window.t('contextMenu.pinConversation');
            } else if (pinMenuText) {
                pinMenuText.textContent = isPinned ? 'Unpin' : 'Chat';
            }
        } catch (error) {
            console.error('ChatStatusFailed:', error);
            const pinMenuText = document.getElementById('pin-conversation-menu-text');
            if (pinMenuText && typeof window.t === 'function') {
                pinMenuText.textContent = window.t('contextMenu.pinConversation');
            } else if (pinMenuText) {
                pinMenuText.textContent = 'Chat';
            }
        }
    } else {
        const pinMenuText = document.getElementById('pin-conversation-menu-text');
        if (pinMenuText && typeof window.t === 'function') {
            pinMenuText.textContent = window.t('contextMenu.pinConversation');
        } else if (pinMenuText) {
            pinMenuText.textContent = 'Chat';
        }
    }

    // Internal UI state handling.
    menu.style.display = 'block';
    menu.style.visibility = 'visible';
    menu.style.opacity = '1';
    
    // Internal UI state handling.
    void menu.offsetHeight;
    
    // Internal UI state handling.
    const menuRect = menu.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    
    // Internal UI state handling.
    const submenuWidth = submenu ? 180 : 0; // Internal UI state handling.
    
    let left = event.clientX;
    let top = event.clientY;
    
    // Internal UI state handling.
    // Internal UI state handling.
    if (left + menuRect.width + submenuWidth > viewportWidth) {
        left = event.clientX - menuRect.width;
        // Internal UI state handling.
        if (left < 0) {
            left = Math.max(8, event.clientX - menuRect.width - submenuWidth);
        }
    }
    
    // Internal UI state handling.
    if (top + menuRect.height > viewportHeight) {
        top = Math.max(8, event.clientY - menuRect.height);
    }
    
    // Internal UI state handling.
    if (left < 0) {
        left = 8;
    }
    
    // Internal UI state handling.
    if (top < 0) {
        top = 8;
    }
    
    menu.style.left = left + 'px';
    menu.style.top = top + 'px';
    
    // Internal UI state handling.
    if (left < event.clientX) {
        if (submenu) {
            submenu.style.left = 'auto';
            submenu.style.right = '100%';
            submenu.style.marginLeft = '0';
            submenu.style.marginRight = '4px';
        }
        if (downloadSubmenu) {
            downloadSubmenu.style.left = 'auto';
            downloadSubmenu.style.right = '100%';
            downloadSubmenu.style.marginLeft = '0';
            downloadSubmenu.style.marginRight = '4px';
        }
    } else {
        if (submenu) {
            submenu.style.left = '100%';
            submenu.style.right = 'auto';
            submenu.style.marginLeft = '4px';
            submenu.style.marginRight = '0';
        }
        if (downloadSubmenu) {
            downloadSubmenu.style.left = '100%';
            downloadSubmenu.style.right = 'auto';
            downloadSubmenu.style.marginLeft = '4px';
            downloadSubmenu.style.marginRight = '0';
        }
    }

    // Internal UI state handling.
    const closeMenu = (e) => {
        // Internal UI state handling.
        const moveToGroupSubmenuEl = document.getElementById('move-to-group-submenu');
        const downloadMarkdownSubmenuEl = document.getElementById('download-markdown-submenu');
        const clickedInMenu = menu.contains(e.target);
        const clickedInSubmenu = moveToGroupSubmenuEl && moveToGroupSubmenuEl.contains(e.target);
        const clickedInDownloadSubmenu = downloadMarkdownSubmenuEl && downloadMarkdownSubmenuEl.contains(e.target);
        
        if (!clickedInMenu && !clickedInSubmenu && !clickedInDownloadSubmenu) {
            // Internal UI state handling.
            closeContextMenu();
            document.removeEventListener('click', closeMenu);
        }
    };
    setTimeout(() => {
        document.addEventListener('click', closeMenu);
    }, 0);
}

// Internal UI state handling.
async function showGroupContextMenu(event, groupId) {
    const menu = document.getElementById('group-context-menu');
    if (!menu) return;

    contextMenuGroupId = groupId;

    // Internal UI state handling.
    try {
        // Internal UI state handling.
        let group = groupsCache.find(g => g.id === groupId);
        let isPinned = false;
        
        if (group) {
            isPinned = group.pinned || false;
        } else {
            // Internal UI state handling.
            const response = await apiFetch(`/api/groups/${groupId}`);
            if (response.ok) {
                group = await response.json();
                isPinned = group.pinned || false;
            }
        }
        
        // Internal UI state handling.
        const pinMenuText = document.getElementById('pin-group-menu-text');
        if (pinMenuText && typeof window.t === 'function') {
            pinMenuText.textContent = isPinned ? window.t('contextMenu.unpinGroup') : window.t('contextMenu.pinGroup');
        } else if (pinMenuText) {
            pinMenuText.textContent = isPinned ? 'Unpin' : 'group';
        }
    } catch (error) {
        console.error('groupStatusFailed:', error);
        const pinMenuText = document.getElementById('pin-group-menu-text');
        if (pinMenuText && typeof window.t === 'function') {
            pinMenuText.textContent = window.t('contextMenu.pinGroup');
        } else if (pinMenuText) {
            pinMenuText.textContent = 'group';
        }
    }

    // Internal UI state handling.
    menu.style.display = 'block';
    menu.style.visibility = 'visible';
    menu.style.opacity = '1';
    
    // Internal UI state handling.
    void menu.offsetHeight;
    
    // Internal UI state handling.
    const menuRect = menu.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    
    let left = event.clientX;
    let top = event.clientY;
    
    // Internal UI state handling.
    if (left + menuRect.width > viewportWidth) {
        left = event.clientX - menuRect.width;
    }
    
    // Internal UI state handling.
    if (top + menuRect.height > viewportHeight) {
        top = event.clientY - menuRect.height;
    }
    
    // Internal UI state handling.
    if (left < 0) {
        left = 8;
    }
    
    // Internal UI state handling.
    if (top < 0) {
        top = 8;
    }
    
    menu.style.left = left + 'px';
    menu.style.top = top + 'px';

    // Internal UI state handling.
    const closeMenu = (e) => {
        if (!menu.contains(e.target)) {
            menu.style.display = 'none';
            document.removeEventListener('click', closeMenu);
        }
    };
    setTimeout(() => {
        document.addEventListener('click', closeMenu);
    }, 0);
}

// Internal UI state handling.
async function renameConversation() {
    const convId = contextMenuConversationId;
    if (!convId) return;

    const newTitle = prompt('Error:', '');
    if (newTitle === null || !newTitle.trim()) {
        closeContextMenu();
        return;
    }

    try {
        const response = await apiFetch(`/api/conversations/${convId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ title: newTitle.trim() }),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'UpdatedFailed');
        }

        // Internal UI state handling.
        const item = document.querySelector(`[data-conversation-id="${convId}"]`);
        if (item) {
            const titleEl = item.querySelector('.conversation-title');
            if (titleEl) {
                titleEl.textContent = newTitle.trim();
            }
        }

        // Internal UI state handling.
        const groupItem = document.querySelector(`.group-conversation-item[data-conversation-id="${convId}"]`);
        if (groupItem) {
            const groupTitleEl = groupItem.querySelector('.group-conversation-title');
            if (groupTitleEl) {
                groupTitleEl.textContent = newTitle.trim();
            }
        }

        // Internal UI state handling.
        loadConversationsWithGroups();
    } catch (error) {
        console.error('ChatFailed:', error);
        const failedLabel = typeof window.t === 'function' ? window.t('chat.renameFailed') : 'Failed';
        const unknownErr = typeof window.t === 'function' ? window.t('createGroupModal.unknownError') : 'UnknownError';
        alert(failedLabel + ': ' + (error.message || unknownErr));
    }

    closeContextMenu();
}

// Internal UI state handling.
async function pinConversation() {
    const convId = contextMenuConversationId;
    if (!convId) return;

    try {
        // Internal UI state handling.
        // Internal UI state handling.
        // Internal UI state handling.
        const conversationGroupId = conversationGroupMappingCache[convId];
        const isInCurrentGroup = currentGroupId && conversationGroupId === currentGroupId;
        
        // Internal UI state handling.
        if (isInCurrentGroup) {
            // Internal UI state handling.
            const response = await apiFetch(`/api/groups/${currentGroupId}/conversations`);
            const groupConvs = await response.json();
            const conv = groupConvs.find(c => c.id === convId);
            
            // Internal UI state handling.
            const currentPinned = conv && conv.groupPinned !== undefined ? conv.groupPinned : false;
            const newPinned = !currentPinned;

            // Internal UI state handling.
            await apiFetch(`/api/groups/${currentGroupId}/conversations/${convId}/pinned`, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ pinned: newPinned }),
            });

            // Internal UI state handling.
            loadGroupConversations(currentGroupId);
        } else {
            // Internal UI state handling.
            const response = await apiFetch(`/api/conversations/${convId}`);
            const conv = await response.json();
            const newPinned = !conv.pinned;

            // Internal UI state handling.
            await apiFetch(`/api/conversations/${convId}/pinned`, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ pinned: newPinned }),
            });

            loadConversationsWithGroups();
        }
    } catch (error) {
        console.error('ChatFailed:', error);
        alert('Failed: ' + (error.message || 'UnknownError'));
    }

    closeContextMenu();
}

// Internal UI state handling.
async function showMoveToGroupSubmenu() {
    const submenu = document.getElementById('move-to-group-submenu');
    if (!submenu) return;

    // Internal UI state handling.
    if (submenuVisible && submenu.style.display === 'block') {
        return;
    }

    // Internal UI state handling.
    if (submenuLoading) {
        return;
    }

    // Internal UI state handling.
    clearSubmenuHideTimeout();
    
    // Internal UI state handling.
    submenuLoading = true;
    submenu.innerHTML = '';

    // Internal UI state handling.
    try {
        // Internal UI state handling.
        if (!Array.isArray(groupsCache) || groupsCache.length === 0) {
            await loadGroups();
        } else {
            // Internal UI state handling.
            // Internal UI state handling.
            try {
                const response = await apiFetch('/api/groups');
                if (response.ok) {
                    const freshGroups = await response.json();
                    if (Array.isArray(freshGroups)) {
                        groupsCache = freshGroups;
                    }
                }
            } catch (err) {
                // Internal UI state handling.
                console.warn('Failed to refresh groups, using cache:', err);
            }
        }
        
        // Internal UI state handling.
        if (!Array.isArray(groupsCache)) {
            console.warn('groupsCache is invalid; reset to empty array');
            groupsCache = [];
            // Internal UI state handling.
            if (groupsCache.length === 0) {
                await loadGroups();
            }
        }
    } catch (error) {
        console.error('Risk scoreFailed:', error);
        // Internal UI state handling.
    }

    // Internal UI state handling.
    if (currentGroupId && contextMenuConversationId) {
        // Internal UI state handling.
        const convInGroup = conversationGroupMappingCache[contextMenuConversationId] === currentGroupId;
        if (convInGroup) {
            const removeItem = document.createElement('div');
            removeItem.className = 'context-submenu-item';
            removeItem.innerHTML = `
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                    <path d="M9 12l6 6M15 12l-6 6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
                <span>text</span>
            `;
            removeItem.onclick = () => {
                removeConversationFromGroup(contextMenuConversationId, currentGroupId);
            };
            submenu.appendChild(removeItem);
            
            // Internal UI state handling.
            const divider = document.createElement('div');
            divider.className = 'context-menu-divider';
            submenu.appendChild(divider);
        }
    }

    // Internal UI state handling.
    if (!Array.isArray(groupsCache)) {
        console.warn('groupsCache is invalid; reset to empty array');
        groupsCache = [];
    }

    // Internal UI state handling.
    if (groupsCache.length > 0) {
        // Internal UI state handling.
        const conversationCurrentGroupId = contextMenuConversationId 
            ? conversationGroupMappingCache[contextMenuConversationId] 
            : null;
        
        groupsCache.forEach(group => {
            // Internal UI state handling.
            if (!group || !group.id || !group.name) {
                console.warn('group:', group);
                return;
            }
            
            // Internal UI state handling.
            if (conversationCurrentGroupId && group.id === conversationCurrentGroupId) {
                return;
            }
            
            const item = document.createElement('div');
            item.className = 'context-submenu-item';
            item.innerHTML = `
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
                <span>${group.name}</span>
            `;
            item.onclick = () => {
                moveConversationToGroup(contextMenuConversationId, group.id);
            };
            submenu.appendChild(item);
        });
    } else {
        // Internal UI state handling.
        console.warn('showMoveToGroupSubmenu: groupsCache is empty; cannot show group list');
    }

    // Internal UI state handling.
    const addGroupLabel = typeof window.t === 'function' ? window.t('chat.addNewGroup') : '+ group';
    const addItem = document.createElement('div');
    addItem.className = 'context-submenu-item add-group-item';
    addItem.innerHTML = `
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
        <span>${addGroupLabel}</span>
    `;
    addItem.onclick = () => {
        showCreateGroupModal(true);
    };
    submenu.appendChild(addItem);

    submenu.style.display = 'block';
    submenuVisible = true;
    submenuLoading = false;
    
    // Internal UI state handling.
    setTimeout(() => {
        const submenuRect = submenu.getBoundingClientRect();
        const viewportWidth = window.innerWidth;
        const viewportHeight = window.innerHeight;
        
        // Internal UI state handling.
        if (submenuRect.right > viewportWidth) {
            submenu.style.left = 'auto';
            submenu.style.right = '100%';
            submenu.style.marginLeft = '0';
            submenu.style.marginRight = '4px';
        }
        
        // Internal UI state handling.
        if (submenuRect.bottom > viewportHeight) {
            const overflow = submenuRect.bottom - viewportHeight;
            const currentTop = parseInt(submenu.style.top) || 0;
            submenu.style.top = (currentTop - overflow - 8) + 'px';
        }
    }, 0);
}

// Internal UI state handling.
let submenuHideTimeout = null;
// Internal UI state handling.
let submenuShowTimeout = null;
// Internal UI state handling.
let submenuLoading = false;
// Internal UI state handling.
let submenuVisible = false;
// Internal UI state handling.
let downloadMarkdownSubmenuHideTimeout = null;

// Internal UI state handling.
function hideMoveToGroupSubmenu() {
    const submenu = document.getElementById('move-to-group-submenu');
    if (submenu) {
        submenu.style.display = 'none';
        submenuVisible = false;
    }
}

// Internal UI state handling.
function clearSubmenuHideTimeout() {
    if (submenuHideTimeout) {
        clearTimeout(submenuHideTimeout);
        submenuHideTimeout = null;
    }
}

// Internal UI state handling.
function clearSubmenuShowTimeout() {
    if (submenuShowTimeout) {
        clearTimeout(submenuShowTimeout);
        submenuShowTimeout = null;
    }
}

function clearDownloadMarkdownSubmenuHideTimeout() {
    if (downloadMarkdownSubmenuHideTimeout) {
        clearTimeout(downloadMarkdownSubmenuHideTimeout);
        downloadMarkdownSubmenuHideTimeout = null;
    }
}

function showDownloadMarkdownSubmenu() {
    const submenu = document.getElementById('download-markdown-submenu');
    if (!submenu) return;
    clearDownloadMarkdownSubmenuHideTimeout();
    submenu.style.display = 'block';
}

function hideDownloadMarkdownSubmenu() {
    const submenu = document.getElementById('download-markdown-submenu');
    if (!submenu) return;
    submenu.style.display = 'none';
}

function handleDownloadMarkdownSubmenuEnter() {
    clearDownloadMarkdownSubmenuHideTimeout();
    showDownloadMarkdownSubmenu();
}

function handleDownloadMarkdownSubmenuLeave(event) {
    const submenu = document.getElementById('download-markdown-submenu');
    if (!submenu) return;
    const relatedTarget = event.relatedTarget;
    if (relatedTarget && submenu.contains(relatedTarget)) {
        return;
    }
    clearDownloadMarkdownSubmenuHideTimeout();
    downloadMarkdownSubmenuHideTimeout = setTimeout(() => {
        hideDownloadMarkdownSubmenu();
        downloadMarkdownSubmenuHideTimeout = null;
    }, 200);
}

// Internal UI state handling.
function handleMoveToGroupSubmenuEnter() {
    // Internal UI state handling.
    clearSubmenuHideTimeout();
    
    // Internal UI state handling.
    const submenu = document.getElementById('move-to-group-submenu');
    if (submenu && submenuVisible && submenu.style.display === 'block') {
        return;
    }
    
    // Internal UI state handling.
    clearSubmenuShowTimeout();
    
    // Internal UI state handling.
    submenuShowTimeout = setTimeout(() => {
        showMoveToGroupSubmenu();
        submenuShowTimeout = null;
    }, 100);
}

// Internal UI state handling.
function handleMoveToGroupSubmenuLeave(event) {
    const submenu = document.getElementById('move-to-group-submenu');
    if (!submenu) return;
    
    // Internal UI state handling.
    clearSubmenuShowTimeout();
    
    // Internal UI state handling.
    const relatedTarget = event.relatedTarget;
    if (relatedTarget && submenu.contains(relatedTarget)) {
        // Internal UI state handling.
        return;
    }
    
    // Internal UI state handling.
    clearSubmenuHideTimeout();
    
    // Internal UI state handling.
    submenuHideTimeout = setTimeout(() => {
        hideMoveToGroupSubmenu();
        submenuHideTimeout = null;
    }, 200);
}

// Internal UI state handling.
async function moveConversationToGroup(convId, groupId) {
    try {
        await apiFetch('/api/groups/conversations', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                conversationId: convId,
                groupId: groupId,
            }),
        });

        // Internal UI state handling.
        const oldGroupId = conversationGroupMappingCache[convId];
        conversationGroupMappingCache[convId] = groupId;
        
        // Internal UI state handling.
        pendingGroupMappings[convId] = groupId;
        
        // Internal UI state handling.
        if (currentConversationId === convId) {
            currentConversationGroupId = groupId;
        }
        
        // Internal UI state handling.
        if (currentGroupId) {
            // Internal UI state handling.
            if (currentGroupId === oldGroupId || currentGroupId === groupId) {
                await loadGroupConversations(currentGroupId);
            }
        }
        
        // Internal UI state handling.
        // Internal UI state handling.
        // Internal UI state handling.
        // Internal UI state handling.
        await loadConversationsWithGroups();
        
        // Internal UI state handling.
        // Internal UI state handling.
        
        // Internal UI state handling.
        await loadGroups();
    } catch (error) {
        console.error('ChatminutesFailed:', error);
        alert('Failed: ' + (error.message || 'UnknownError'));
    }

    closeContextMenu();
}

// Internal UI state handling.
async function removeConversationFromGroup(convId, groupId) {
    try {
        await apiFetch(`/api/groups/${groupId}/conversations/${convId}`, {
            method: 'DELETE',
        });

        // Internal UI state handling.
        delete conversationGroupMappingCache[convId];
        // Internal UI state handling.
        delete pendingGroupMappings[convId];
        
        // Internal UI state handling.
        if (currentConversationId === convId) {
            currentConversationGroupId = null;
        }
        
        // Internal UI state handling.
        if (currentGroupId === groupId) {
            await loadGroupConversations(groupId);
        }
        
        // Internal UI state handling.
        await loadConversationGroupMapping();
        
        // Internal UI state handling.
        await loadGroups();
        
        // Internal UI state handling.
        // Internal UI state handling.
        const savedGroupId = currentGroupId;
        currentGroupId = null;
        await loadConversationsWithGroups();
        currentGroupId = savedGroupId;
    } catch (error) {
        console.error('Risk scoreChatFailed:', error);
        alert('Failed: ' + (error.message || 'UnknownError'));
    }

    closeContextMenu();
}

// Internal UI state handling.
async function loadConversationGroupMapping() {
    try {
        // Internal UI state handling.
        const response = await apiFetch('/api/groups/mappings');

        // Internal UI state handling.
        const preservedMappings = { ...pendingGroupMappings };

        conversationGroupMappingCache = {};

        if (response.ok) {
            const mappings = await response.json();
            if (Array.isArray(mappings)) {
                mappings.forEach(m => {
                    conversationGroupMappingCache[m.conversationId] = m.groupId;
                    // Internal UI state handling.
                    if (preservedMappings[m.conversationId] === m.groupId) {
                        delete pendingGroupMappings[m.conversationId];
                    }
                });
            }
        }

        // Internal UI state handling.
        Object.assign(conversationGroupMappingCache, preservedMappings);
    } catch (error) {
        console.error('Failed to load conversation groups:', error);
    }
}

// Internal UI state handling.
function showAttackChainFromContext() {
    const convId = contextMenuConversationId;
    if (!convId) return;
    
    closeContextMenu();
    showAttackChain(convId);
}

function formatConversationDateForMarkdown(value) {
    if (!value) return '';
    const d = new Date(value);
    if (isNaN(d.getTime())) return '';
    const locale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
    return d.toLocaleString(locale, {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
    });
}

function getConversationRoleLabel(role) {
    switch (role) {
        case 'assistant':
            return 'Assistant';
        case 'user':
            return 'User';
        case 'system':
            return 'System';
        default:
            return role || 'Unknown';
    }
}

function formatConversationAsMarkdown(conversation, options = {}) {
    const includeToolDetails = !!options.includeToolDetails;
    const title = (conversation && conversation.title ? String(conversation.title) : '').trim() || 'Untitled Conversation';
    const createdAt = formatConversationDateForMarkdown(conversation && conversation.createdAt);
    const updatedAt = formatConversationDateForMarkdown(conversation && conversation.updatedAt);
    const messages = Array.isArray(conversation && conversation.messages) ? conversation.messages : [];

    let markdown = `# ${title}\n\n`;
    markdown += `- Conversation ID: \`${conversation && conversation.id ? conversation.id : ''}\`\n`;
    if (createdAt) markdown += `- Created At: ${createdAt}\n`;
    if (updatedAt) markdown += `- Updated At: ${updatedAt}\n`;
    markdown += `- Message Count: ${messages.length}\n\n`;
    markdown += '---\n\n';

    if (messages.length === 0) {
        markdown += '_No messages in this conversation._\n';
        return markdown;
    }

    messages.forEach((msg, index) => {
        if (msg && msg.role === 'user' && isInterruptContinueInjectChatMessage(msg.content)) {
            return;
        }
        const role = getConversationRoleLabel(msg && msg.role);
        const timestamp = formatConversationDateForMarkdown(msg && msg.createdAt);
        const content = msg && typeof msg.content === 'string' ? msg.content : '';

        markdown += `## ${index + 1}. ${role}`;
        if (timestamp) markdown += ` (${timestamp})`;
        markdown += '\n\n';
        markdown += content ? `${content}\n\n` : '_[Empty message]_\n\n';

        if (Array.isArray(msg && msg.processDetails) && msg.processDetails.length > 0) {
            markdown += '### Process Details\n\n';
            msg.processDetails.forEach((detail) => {
                const detailTime = formatConversationDateForMarkdown(detail && detail.timestamp);
                const eventType = detail && detail.eventType ? detail.eventType : 'event';
                const detailMsg = detail && detail.message ? detail.message : '';
                // Avoid "[label]:" pattern because some Markdown parsers treat it as link reference definition.
                markdown += `- \`${eventType}\``;
                if (detailTime) markdown += ` ${detailTime}`;
                if (detailMsg) markdown += `: ${detailMsg}`;
                markdown += '\n';

                if (includeToolDetails && detail && detail.data && (eventType === 'tool_call' || eventType === 'tool_result')) {
                    const pretty = JSON.stringify(detail.data, null, 2);
                    markdown += '\n```json\n';
                    markdown += pretty || '{}';
                    markdown += '\n```\n';
                }
            });
            markdown += '\n';
        }

        if (Array.isArray(msg && msg.mcpExecutionIds) && msg.mcpExecutionIds.length > 0) {
            markdown += `- MCP Execution IDs: ${msg.mcpExecutionIds.join(', ')}\n\n`;
        }

        markdown += '---\n\n';
    });

    return markdown;
}

function buildConversationMarkdownFileName(conversation, options = {}) {
    const includeToolDetails = !!options.includeToolDetails;
    const title = (conversation && conversation.title ? String(conversation.title) : '').trim() || 'conversation';
    const safeTitle = title
        .replace(/[\\/:*?"<>|]/g, '_')
        .replace(/\s+/g, '_')
        .slice(0, 60) || 'conversation';
    const idPart = (conversation && conversation.id ? String(conversation.id) : '').slice(0, 8) || 'export';
    const modePart = includeToolDetails ? 'full' : 'summary';
    return `${safeTitle}_${idPart}_${modePart}.md`;
}

// Internal UI state handling.
async function downloadConversationMarkdownFromContext(includeToolDetails = false) {
    const convId = contextMenuConversationId;
    if (!convId) return;

    try {
        // Internal UI state handling.
        const response = await apiFetch(`/api/conversations/${convId}?include_process_details=1`);
        let conversation = null;
        try {
            conversation = await response.json();
        } catch (e) {
            conversation = null;
        }
        if (!response.ok) {
            const errorMsg = conversation && conversation.error ? conversation.error : 'unknown error';
            throw new Error(errorMsg);
        }

        const markdown = formatConversationAsMarkdown(conversation || {}, { includeToolDetails });
        const blob = new Blob([markdown], { type: 'text/markdown;charset=utf-8' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = buildConversationMarkdownFileName(conversation || {}, { includeToolDetails });
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        URL.revokeObjectURL(url);
    } catch (error) {
        console.error('Chat Markdown Failed:', error);
        const failedLabel = typeof window.t === 'function' ? window.t('chat.downloadConversationFailed') : 'Failed';
        const errMsg = error && error.message ? error.message : 'unknown error';
        alert(failedLabel + ': ' + errMsg);
    }

    closeContextMenu();
}

// Internal UI state handling.
function navigateToVulnerabilitiesForContextConversation() {
    const convId = contextMenuConversationId;
    if (!convId) {
        closeContextMenu();
        return;
    }
    closeContextMenu();
    window.location.hash = 'vulnerabilities?conversation_id=' + encodeURIComponent(convId);
}

// Internal UI state handling.
function deleteConversationFromContext() {
    const convId = contextMenuConversationId;
    if (!convId) return;

    const confirmMsg = typeof window.t === 'function' ? window.t('chat.deleteConversationConfirm') : 'Confirm DeleteChat?';
    if (confirm(confirmMsg)) {
        deleteConversation(convId, true); // Internal UI state handling.
    }
    closeContextMenu();
}

// Internal UI state handling.
function closeContextMenu() {
    const menu = document.getElementById('conversation-context-menu');
    if (menu) {
        menu.style.display = 'none';
    }
    const submenu = document.getElementById('move-to-group-submenu');
    if (submenu) {
        submenu.style.display = 'none';
        submenuVisible = false;
    }
    const downloadSubmenu = document.getElementById('download-markdown-submenu');
    if (downloadSubmenu) {
        downloadSubmenu.style.display = 'none';
    }
    // Internal UI state handling.
    clearSubmenuHideTimeout();
    clearSubmenuShowTimeout();
    clearDownloadMarkdownSubmenuHideTimeout();
    submenuLoading = false;
    contextMenuConversationId = null;
}

// Internal UI state handling.
let allConversationsForBatch = [];

// Internal UI state handling.
function updateBatchManageTitle(count) {
    const titleEl = document.getElementById('batch-manage-title');
    if (!titleEl || typeof window.t !== 'function') return;
    const template = window.t('batchManageModal.title', { count: '__C__' });
    const parts = template.split('__C__');
    titleEl.innerHTML = (parts[0] || '') + '<span id="batch-manage-count">' + (count || 0) + '</span>' + (parts[1] || '');
}

async function showBatchManageModal() {
    try {
        const response = await apiFetch('/api/conversations?limit=1000');
        
        // Internal UI state handling.
        if (!response.ok) {
            allConversationsForBatch = [];
        } else {
            const data = await response.json();
            allConversationsForBatch = Array.isArray(data) ? data : [];
        }

        const modal = document.getElementById('batch-manage-modal');
        updateBatchManageTitle(allConversationsForBatch.length);

        renderBatchConversations();
        if (modal) {
            modal.style.display = 'flex';
        }
    } catch (error) {
        console.error('ChatFailed:', error);
        // Internal UI state handling.
        allConversationsForBatch = [];
        const modal = document.getElementById('batch-manage-modal');
        updateBatchManageTitle(0);
        if (modal) {
            renderBatchConversations();
            modal.style.display = 'flex';
        }
    }
}

// Internal UI state handling.
function safeTruncateText(text, maxLength = 50) {
    if (!text || typeof text !== 'string') {
        return text || '';
    }
    
    // Internal UI state handling.
    const chars = Array.from(text);
    
    // Internal UI state handling.
    if (chars.length <= maxLength) {
        return text;
    }
    
    // Internal UI state handling.
    let truncatedChars = chars.slice(0, maxLength);
    
    // Internal UI state handling.
    // Internal UI state handling.
    const searchRange = Math.floor(maxLength * 0.2);
    const breakChars = [', ', '.', ',', ' ', ',', '.', ';', ':', '!', '?', '!', '?', '/', '\\', '-', '_'];
    let bestBreakPos = truncatedChars.length;
    
    for (let i = truncatedChars.length - 1; i >= truncatedChars.length - searchRange && i >= 0; i--) {
        if (breakChars.includes(truncatedChars[i])) {
            bestBreakPos = i + 1; // Internal UI state handling.
            break;
        }
    }
    
    // Internal UI state handling.
    if (bestBreakPos < truncatedChars.length) {
        truncatedChars = truncatedChars.slice(0, bestBreakPos);
    }
    
    // Internal UI state handling.
    return truncatedChars.join('') + '...';
}

// Internal UI state handling.
function renderBatchConversations(filtered = null) {
    const list = document.getElementById('batch-conversations-list');
    if (!list) return;

    const conversations = filtered || allConversationsForBatch;
    list.innerHTML = '';

    conversations.forEach(conv => {
        const row = document.createElement('div');
        row.className = 'batch-conversation-row';
        row.dataset.conversationId = conv.id;

        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.className = 'batch-conversation-checkbox';
        checkbox.dataset.conversationId = conv.id;

        const name = document.createElement('div');
        name.className = 'batch-table-col-name';
        const originalTitle = conv.title || (typeof window.t === 'function' ? window.t('batchManageModal.unnamedConversation') : 'Chat');
        // Internal UI state handling.
        const truncatedTitle = safeTruncateText(originalTitle, 45);
        name.textContent = truncatedTitle;
        // Internal UI state handling.
        name.title = originalTitle;

        const time = document.createElement('div');
        time.className = 'batch-table-col-time';
        const dateObj = conv.updatedAt ? new Date(conv.updatedAt) : new Date();
        const locale = (typeof i18next !== 'undefined' && i18next.language) ? i18next.language : 'zh-CN';
        time.textContent = dateObj.toLocaleString(locale, {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit'
        });

        const action = document.createElement('div');
        action.className = 'batch-table-col-action';
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'batch-delete-btn';
        deleteBtn.innerHTML = '🗑️';
        deleteBtn.onclick = () => deleteConversation(conv.id);
        action.appendChild(deleteBtn);

        row.appendChild(checkbox);
        row.appendChild(name);
        row.appendChild(time);
        row.appendChild(action);

        list.appendChild(row);
    });
}

// FilterBatch manageChat
function filterBatchConversations(query) {
    if (!query || !query.trim()) {
        renderBatchConversations();
        return;
    }

    const filtered = allConversationsForBatch.filter(conv => {
        const title = (conv.title || '').toLowerCase();
        return title.includes(query.toLowerCase());
    });

    renderBatchConversations(filtered);
}

// Select all/Deselect all
function toggleSelectAllBatch() {
    const selectAll = document.getElementById('batch-select-all');
    const checkboxes = document.querySelectorAll('.batch-conversation-checkbox');
    
    checkboxes.forEach(cb => {
        cb.checked = selectAll.checked;
    });
}

// Internal UI state handling.
async function deleteSelectedConversations() {
    const checkboxes = document.querySelectorAll('.batch-conversation-checkbox:checked');
    if (checkboxes.length === 0) {
        alert(typeof window.t === 'function' ? window.t('batchManageModal.confirmDeleteNone') : 'Delete chat');
        return;
    }

    const confirmMsg = typeof window.t === 'function' ? window.t('batchManageModal.confirmDeleteN', { count: checkboxes.length }) : 'Confirm Delete ' + checkboxes.length + ' Chat?';
    if (!confirm(confirmMsg)) {
        return;
    }

    const ids = Array.from(checkboxes).map(cb => cb.dataset.conversationId);
    
    try {
        for (const id of ids) {
            await deleteConversation(id, true); // Internal UI state handling.
        }
        closeBatchManageModal();
        loadConversationsWithGroups();
    } catch (error) {
        console.error('DeleteFailed:', error);
        const failedMsg = typeof window.t === 'function' ? window.t('batchManageModal.deleteFailed') : 'DeleteFailed';
        const unknownErr = typeof window.t === 'function' ? window.t('createGroupModal.unknownError') : 'UnknownError';
        alert(failedMsg + ': ' + (error.message || unknownErr));
    }
}

// Internal UI state handling.
function closeBatchManageModal() {
    const modal = document.getElementById('batch-manage-modal');
    if (modal) {
        modal.style.display = 'none';
    }
    const selectAll = document.getElementById('batch-select-all');
    if (selectAll) {
        selectAll.checked = false;
    }
    allConversationsForBatch = [];
}

// Internal UI state handling.
function refreshChatPanelI18n() {
    const locale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
    const timeOpts = { hour: '2-digit', minute: '2-digit' };
    if (locale === 'zh-CN') timeOpts.hour12 = false;
    const t = typeof window.t === 'function' ? window.t : function (k) { return k; };

    const messagesEl = document.getElementById('chat-messages');
    if (messagesEl) {
        messagesEl.querySelectorAll('.message-time[data-message-time]').forEach(function (el) {
            try {
                const d = new Date(el.dataset.messageTime);
                if (!isNaN(d.getTime())) {
                    el.textContent = d.toLocaleTimeString(locale, timeOpts);
                }
            } catch (e) { /* ignore */ }
        });
        messagesEl.querySelectorAll('.mcp-call-label').forEach(function (el) {
            el.textContent = '\uD83D\uDCCB ' + t('chat.penetrationTestDetail');
        });
        messagesEl.querySelectorAll('.process-detail-btn').forEach(function (btn) {
            const span = btn.querySelector('span');
            if (!span) return;
            const assistantEl = btn.closest('.message.assistant');
            const messageId = assistantEl && assistantEl.id;
            const detailsId = messageId ? 'process-details-' + messageId : '';
            const timeline = detailsId ? document.getElementById(detailsId) && document.getElementById(detailsId).querySelector('.progress-timeline') : null;
            const expanded = timeline && timeline.classList.contains('expanded');
            span.textContent = expanded ? t('tasks.collapseDetail') : t('chat.expandDetail');
        });
    }

    const mcpModal = document.getElementById('mcp-detail-modal');
    if (mcpModal && mcpModal.style.display === 'block') {
        const detailTimeEl = document.getElementById('detail-time');
        if (detailTimeEl && detailTimeEl.dataset.detailTimeIso) {
            try {
                const d = new Date(detailTimeEl.dataset.detailTimeIso);
                if (!isNaN(d.getTime())) {
                    detailTimeEl.textContent = d.toLocaleString(locale);
                }
            } catch (e) { /* ignore */ }
        }
        const statusEl = document.getElementById('detail-status');
        if (statusEl && statusEl.dataset.detailStatus !== undefined && typeof getStatusText === 'function') {
            statusEl.textContent = getStatusText(statusEl.dataset.detailStatus);
        }
    }
}

// Internal UI state handling.
document.addEventListener('languagechange', function () {
    refreshSystemReadyMessageBubbles();
    refreshChatPanelI18n();
    const modal = document.getElementById('batch-manage-modal');
    if (modal && modal.style.display === 'flex') {
        updateBatchManageTitle(allConversationsForBatch.length);
    }
    // Internal UI state handling.
    if (typeof loadConversationsWithGroups === 'function') {
        loadConversationsWithGroups();
    } else if (typeof loadConversations === 'function') {
        loadConversations();
    }
});

// Internal UI state handling.
function showCreateGroupModal(andMoveConversation = false) {
    const modal = document.getElementById('create-group-modal');
    const input = document.getElementById('create-group-name-input');
    const iconBtn = document.getElementById('create-group-icon-btn');
    const iconPicker = document.getElementById('group-icon-picker');
    const customInput = document.getElementById('custom-icon-input');
    
    if (input) {
        input.value = '';
    }
    // Internal UI state handling.
    if (iconBtn) {
        iconBtn.textContent = '📁';
    }
    // Internal UI state handling.
    if (customInput) {
        customInput.value = '';
    }
    // Internal UI state handling.
    if (iconPicker) {
        iconPicker.style.display = 'none';
    }
    if (modal) {
        modal.style.display = 'flex';
        modal.dataset.moveConversation = andMoveConversation ? 'true' : 'false';
        if (input) {
            setTimeout(() => input.focus(), 100);
        }
    }
}

// Internal UI state handling.
function closeCreateGroupModal() {
    const modal = document.getElementById('create-group-modal');
    if (modal) {
        modal.style.display = 'none';
    }
    const input = document.getElementById('create-group-name-input');
    if (input) {
        input.value = '';
    }
    // Internal UI state handling.
    const iconBtn = document.getElementById('create-group-icon-btn');
    if (iconBtn) {
        iconBtn.textContent = '📁';
    }
    // Internal UI state handling.
    const customInput = document.getElementById('custom-icon-input');
    if (customInput) {
        customInput.value = '';
    }
    // Internal UI state handling.
    const iconPicker = document.getElementById('group-icon-picker');
    if (iconPicker) {
        iconPicker.style.display = 'none';
    }
}

// Internal UI state handling.
function selectSuggestion(name) {
    const input = document.getElementById('create-group-name-input');
    if (input) {
        input.value = name;
        input.focus();
    }
}

// Internal UI state handling.
function selectSuggestionByKey(i18nKey) {
    const input = document.getElementById('create-group-name-input');
    if (input && typeof window.t === 'function') {
        input.value = window.t(i18nKey);
        input.focus();
    }
}

// Internal UI state handling.
function toggleGroupIconPicker() {
    const picker = document.getElementById('group-icon-picker');
    if (picker) {
        const isVisible = picker.style.display !== 'none';
        picker.style.display = isVisible ? 'none' : 'block';
    }
}

// Internal UI state handling.
function selectGroupIcon(icon) {
    const iconBtn = document.getElementById('create-group-icon-btn');
    if (iconBtn) {
        iconBtn.textContent = icon;
    }
    // Internal UI state handling.
    const customInput = document.getElementById('custom-icon-input');
    if (customInput) {
        customInput.value = '';
    }
    // Internal UI state handling.
    const picker = document.getElementById('group-icon-picker');
    if (picker) {
        picker.style.display = 'none';
    }
}

// Internal UI state handling.
function applyCustomIcon() {
    const customInput = document.getElementById('custom-icon-input');
    if (!customInput) return;
    
    const customIcon = customInput.value.trim();
    if (!customIcon) {
        return;
    }
    
    const iconBtn = document.getElementById('create-group-icon-btn');
    if (iconBtn) {
        iconBtn.textContent = customIcon;
    }
    
    // Internal UI state handling.
    customInput.value = '';
    const picker = document.getElementById('group-icon-picker');
    if (picker) {
        picker.style.display = 'none';
    }
}

// Internal UI state handling.
document.addEventListener('DOMContentLoaded', function() {
    const customInput = document.getElementById('custom-icon-input');
    if (customInput) {
        customInput.addEventListener('keydown', function(e) {
            if (e.key === 'Enter') {
                e.preventDefault();
                applyCustomIcon();
            }
        });
    }
    initChatAgentModeFromConfig()
        .then(function () {
            refreshHitlConfigByCurrentConversation();
        })
        .catch(function () {
            refreshHitlConfigByCurrentConversation();
        });
});

document.addEventListener('languagechange', function () {
    refreshHitlConfigByCurrentConversation();
});

// Internal UI state handling.
document.addEventListener('click', function(event) {
    const picker = document.getElementById('group-icon-picker');
    const iconBtn = document.getElementById('create-group-icon-btn');
    if (picker && iconBtn) {
        // Internal UI state handling.
        if (!picker.contains(event.target) && !iconBtn.contains(event.target)) {
            picker.style.display = 'none';
        }
    }

    const agentWrap = document.getElementById('agent-mode-wrapper');
    const agentPanel = document.getElementById('agent-mode-panel');
    if (agentWrap && agentPanel && agentPanel.style.display === 'flex') {
        if (!agentWrap.contains(event.target)) {
            closeAgentModePanel();
        }
    }

    const reasoningWrap = document.getElementById('chat-reasoning-wrapper');
    if (reasoningWrap && reasoningWrap.style.display !== 'none' &&
        !reasoningWrap.classList.contains('conversation-reasoning-collapsed')) {
        if (!reasoningWrap.contains(event.target)) {
            closeChatReasoningPanel();
        }
    }
});

// Internal UI state handling.
async function createGroup(event) {
    // Internal UI state handling.
    if (event) {
        event.preventDefault();
        event.stopPropagation();
    }

    const input = document.getElementById('create-group-name-input');
    if (!input) {
        console.error('Copied');
        return;
    }

    const name = input.value.trim();
    if (!name) {
        alert(typeof window.t === 'function' ? window.t('createGroupModal.groupNamePlaceholder') : 'group');
        return;
    }

    // Internal UI state handling.
    try {
        let groups;
        if (Array.isArray(groupsCache) && groupsCache.length > 0) {
            groups = groupsCache;
        } else {
            const response = await apiFetch('/api/groups');
            groups = await response.json();
        }
        
        // Internal UI state handling.
        if (!Array.isArray(groups)) {
            groups = [];
        }
        
        const nameExists = groups.some(g => g.name === name);
        if (nameExists) {
            alert(typeof window.t === 'function' ? window.t('createGroupModal.nameExists') : 'group, text');
            return;
        }
    } catch (error) {
        console.error('Risk scoreFailed:', error);
    }

    // Internal UI state handling.
    const iconBtn = document.getElementById('create-group-icon-btn');
    const selectedIcon = iconBtn ? iconBtn.textContent.trim() : '📁';

    try {
        const response = await apiFetch('/api/groups', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                name: name,
                icon: selectedIcon,
            }),
        });

        if (!response.ok) {
            const error = await response.json();
            const nameExistsMsg = typeof window.t === 'function' ? window.t('createGroupModal.nameExists') : 'group, text';
            if (error.error && error.error.includes('Copied')) {
                alert(nameExistsMsg);
                return;
            }
            const createFailedMsg = typeof window.t === 'function' ? window.t('createGroupModal.createFailed') : 'Failed';
            throw new Error(error.error || createFailedMsg);
        }

        const newGroup = await response.json();
        
        // Internal UI state handling.
        const submenu = document.getElementById('move-to-group-submenu');
        const isSubmenuOpen = submenu && submenu.style.display !== 'none';

        await loadGroups();

        const modal = document.getElementById('create-group-modal');
        const shouldMove = modal && modal.dataset.moveConversation === 'true';
        
        closeCreateGroupModal();

        if (shouldMove && contextMenuConversationId) {
            moveConversationToGroup(contextMenuConversationId, newGroup.id);
        }

        // Internal UI state handling.
        if (isSubmenuOpen) {
            await showMoveToGroupSubmenu();
        }
    } catch (error) {
        console.error('Risk scoreFailed:', error);
        const createFailedMsg = typeof window.t === 'function' ? window.t('createGroupModal.createFailed') : 'Failed';
        const unknownErr = typeof window.t === 'function' ? window.t('createGroupModal.unknownError') : 'UnknownError';
        alert(createFailedMsg + ': ' + (error.message || unknownErr));
    }
}

// Internal UI state handling.
async function enterGroupDetail(groupId) {
    currentGroupId = groupId;
    // Internal UI state handling.
    // Internal UI state handling.
    currentConversationGroupId = null;
    
    try {
        const response = await apiFetch(`/api/groups/${groupId}`);
        const group = await response.json();
        
        if (!group) {
            currentGroupId = null;
            return;
        }

        // Internal UI state handling.
        const sidebar = document.querySelector('.conversation-sidebar');
        const groupDetailPage = document.getElementById('group-detail-page');
        const chatContainer = document.querySelector('.chat-container');
        const titleEl = document.getElementById('group-detail-title');

        // Internal UI state handling.
        if (sidebar) sidebar.style.display = 'flex';
        // Internal UI state handling.
        if (chatContainer) chatContainer.style.display = 'none';
        if (groupDetailPage) groupDetailPage.style.display = 'flex';
        if (titleEl) titleEl.textContent = group.name;

        // Internal UI state handling.
        await loadGroups();

        // Internal UI state handling.
        loadGroupConversations(groupId, currentGroupSearchQuery);
    } catch (error) {
        console.error('Risk scoreFailed:', error);
        currentGroupId = null;
    }
}

// Internal UI state handling.
function exitGroupDetail() {
    currentGroupId = null;
    currentGroupSearchQuery = ''; // Clear searchStatus
    
    // Internal UI state handling.
    const searchContainer = document.getElementById('group-search-container');
    const searchInput = document.getElementById('group-search-input');
    if (searchContainer) searchContainer.style.display = 'none';
    if (searchInput) searchInput.value = '';
    
    const sidebar = document.querySelector('.conversation-sidebar');
    const groupDetailPage = document.getElementById('group-detail-page');
    const chatContainer = document.querySelector('.chat-container');

    // Internal UI state handling.
    if (sidebar) sidebar.style.display = 'flex';
    // Internal UI state handling.
    if (groupDetailPage) groupDetailPage.style.display = 'none';
    if (chatContainer) chatContainer.style.display = 'flex';

    loadConversationsWithGroups();
}

// Internal UI state handling.
async function loadGroupConversations(groupId, searchQuery = '') {
    try {
        if (!groupId) {
            console.error('loadGroupConversations: groupId is null or undefined');
            return;
        }
        
        // Internal UI state handling.
        if (Object.keys(conversationGroupMappingCache).length === 0) {
            await loadConversationGroupMapping();
        }
        
        // Internal UI state handling.
        const list = document.getElementById('group-conversations-list');
        if (!list) {
            console.error('group-conversations-list element not found');
            return;
        }
        
        // Internal UI state handling.
        if (searchQuery) {
            list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">' + (typeof window.t === 'function' ? window.t('chat.searching') : 'Searchtext...') + '</div>';
        } else {
            list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">' + (typeof window.t === 'function' ? window.t('chat.loading') : 'Loading...') + '</div>';
        }

        // Internal UI state handling.
        let url = `/api/groups/${groupId}/conversations`;
        if (searchQuery && searchQuery.trim()) {
            url += '?search=' + encodeURIComponent(searchQuery.trim());
        }
        
        const response = await apiFetch(url);
        if (!response.ok) {
            console.error(`Failed to load conversations for group ${groupId}:`, response.statusText);
            list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">' + (typeof window.t === 'function' ? window.t('chat.loadFailedRetry') : 'Copy failed; please select and copy manually.') + '</div>';
            return;
        }
        
        let groupConvs = await response.json();
        
        // Internal UI state handling.
        if (!groupConvs) {
            groupConvs = [];
        }
        
        // Internal UI state handling.
        if (!Array.isArray(groupConvs)) {
            console.error(`Invalid response for group ${groupId}:`, groupConvs);
            list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">' + (typeof window.t === 'function' ? window.t('chat.dataFormatError') : 'textError') + '</div>';
            return;
        }
        
        // Internal UI state handling.
        // Internal UI state handling.
        Object.keys(conversationGroupMappingCache).forEach(convId => {
            if (conversationGroupMappingCache[convId] === groupId) {
                // Internal UI state handling.
                if (!groupConvs.find(c => c.id === convId)) {
                    delete conversationGroupMappingCache[convId];
                }
            }
        });
        
        // Internal UI state handling.
        groupConvs.forEach(conv => {
            conversationGroupMappingCache[conv.id] = groupId;
        });

        // Internal UI state handling.
        list.innerHTML = '';

        if (groupConvs.length === 0) {
            const emptyMsg = typeof window.t === 'function' ? window.t('chat.emptyGroupConversations') : 'groupNoneChat';
            const noMatchMsg = typeof window.t === 'function' ? window.t('chat.noMatchingConversationsInGroup') : 'Chat';
            if (searchQuery && searchQuery.trim()) {
                list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">' + (noMatchMsg || 'Chat') + '</div>';
            } else {
                list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">' + (emptyMsg || 'groupNoneChat') + '</div>';
            }
            return;
        }

        // Internal UI state handling.
        for (const conv of groupConvs) {
            try {
                // Internal UI state handling.
                if (!conv.id) {
                    console.warn('Conversation missing id:', conv);
                    continue;
                }
                
                const convResponse = await apiFetch(`/api/conversations/${conv.id}`);
                if (!convResponse.ok) {
                    console.error(`Failed to load conversation ${conv.id}:`, convResponse.statusText);
                    continue;
                }
                
                const fullConv = await convResponse.json();
                
                const item = document.createElement('div');
                item.className = 'group-conversation-item';
                item.dataset.conversationId = conv.id;
                // Internal UI state handling.
                // Internal UI state handling.
                if (currentGroupId && conv.id === currentConversationId) {
                    item.classList.add('active');
                } else {
                    item.classList.remove('active');
                }

                // Internal UI state handling.
                const contentWrapper = document.createElement('div');
                contentWrapper.className = 'group-conversation-content-wrapper';

                const titleWrapper = document.createElement('div');
                titleWrapper.style.display = 'flex';
                titleWrapper.style.alignItems = 'center';
                titleWrapper.style.gap = '4px';

                const title = document.createElement('div');
                title.className = 'group-conversation-title';
                const titleText = fullConv.title || conv.title || 'Chat';
                title.textContent = safeTruncateText(titleText, 60);
                title.title = titleText; // Internal UI state handling.
                titleWrapper.appendChild(title);

                // Internal UI state handling.
                if (conv.groupPinned) {
                    const pinIcon = document.createElement('span');
                    pinIcon.className = 'conversation-item-pinned';
                    pinIcon.innerHTML = '📌';
                    pinIcon.title = 'group';
                    titleWrapper.appendChild(pinIcon);
                }

                contentWrapper.appendChild(titleWrapper);

                const timeWrapper = document.createElement('div');
                timeWrapper.className = 'group-conversation-time';
                const dateObj = fullConv.updatedAt ? new Date(fullConv.updatedAt) : new Date();
                const convListLocale = (typeof window.__locale === 'string' && window.__locale.startsWith('zh')) ? 'zh-CN' : 'en-US';
                timeWrapper.textContent = dateObj.toLocaleString(convListLocale, {
                    year: 'numeric',
                    month: 'long',
                    day: 'numeric',
                    hour: '2-digit',
                    minute: '2-digit'
                });

                contentWrapper.appendChild(timeWrapper);

                // Internal UI state handling.
                if (fullConv.messages && fullConv.messages.length > 0) {
                    const firstMsg = fullConv.messages.find(m => m.role === 'user' && m.content);
                    if (firstMsg && firstMsg.content) {
                        const content = document.createElement('div');
                        content.className = 'group-conversation-content';
                        let preview = firstMsg.content.substring(0, 200);
                        if (firstMsg.content.length > 200) {
                            preview += '...';
                        }
                        content.textContent = preview;
                        contentWrapper.appendChild(content);
                    }
                }

                item.appendChild(contentWrapper);

                // Internal UI state handling.
                const menuBtn = document.createElement('button');
                menuBtn.className = 'conversation-item-menu';
                menuBtn.innerHTML = '⋯';
                menuBtn.onclick = (e) => {
                    e.stopPropagation();
                    contextMenuConversationId = conv.id;
                    showConversationContextMenu(e);
                };
                item.appendChild(menuBtn);

                item.onclick = (e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    // Internal UI state handling.
                    const groupDetailPage = document.getElementById('group-detail-page');
                    const chatContainer = document.querySelector('.chat-container');
                    if (groupDetailPage) groupDetailPage.style.display = 'none';
                    if (chatContainer) chatContainer.style.display = 'flex';
                    loadConversation(conv.id);
                };

                list.appendChild(item);
            } catch (err) {
                console.error(`Chat ${conv.id} Failed:`, err);
            }
        }
    } catch (error) {
        console.error('Risk scoreChatFailed:', error);
    }
}

// Internal UI state handling.
async function editGroup() {
    if (!currentGroupId) return;

    try {
        const response = await apiFetch(`/api/groups/${currentGroupId}`);
        const group = await response.json();
        if (!group) return;

        const renamePrompt = typeof window.t === 'function' ? window.t('chat.renameGroupPrompt') : 'Error:';
        const newName = prompt(renamePrompt, group.name);
        if (newName === null || !newName.trim()) return;

        const trimmedName = newName.trim();
        
        // Internal UI state handling.
        let groups;
        if (Array.isArray(groupsCache) && groupsCache.length > 0) {
            groups = groupsCache;
        } else {
            const response = await apiFetch('/api/groups');
            groups = await response.json();
        }
        
        // Internal UI state handling.
        if (!Array.isArray(groups)) {
            groups = [];
        }
        
        const nameExists = groups.some(g => g.name === trimmedName && g.id !== currentGroupId);
        if (nameExists) {
            alert(typeof window.t === 'function' ? window.t('createGroupModal.nameExists') : 'group, text');
            return;
        }

        const updateResponse = await apiFetch(`/api/groups/${currentGroupId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                name: trimmedName,
                icon: group.icon || '📁',
            }),
        });

        if (!updateResponse.ok) {
            const error = await updateResponse.json();
            if (error.error && error.error.includes('Copied')) {
                alert('group, text');
                return;
            }
            throw new Error(error.error || 'UpdatedFailed');
        }

        loadGroups();
        
        const titleEl = document.getElementById('group-detail-title');
        if (titleEl) {
            titleEl.textContent = trimmedName;
        }
    } catch (error) {
        console.error('EditminutesFailed:', error);
        alert('EditFailed: ' + (error.message || 'UnknownError'));
    }
}

// Internal UI state handling.
async function deleteGroup() {
    if (!currentGroupId) return;

    const deleteConfirmMsg = typeof window.t === 'function' ? window.t('chat.deleteGroupConfirm') : 'Confirm Deletegroup?minutesChatDelete, group.';
    if (!confirm(deleteConfirmMsg)) {
        return;
    }

    try {
        await apiFetch(`/api/groups/${currentGroupId}`, {
            method: 'DELETE',
        });

        // Internal UI state handling.
        groupsCache = groupsCache.filter(g => g.id !== currentGroupId);
        Object.keys(conversationGroupMappingCache).forEach(convId => {
            if (conversationGroupMappingCache[convId] === currentGroupId) {
                delete conversationGroupMappingCache[convId];
            }
        });

        // Internal UI state handling.
        const submenu = document.getElementById('move-to-group-submenu');
        if (submenu && submenu.style.display !== 'none') {
            // Internal UI state handling.
            await loadGroups();
            await showMoveToGroupSubmenu();
        } else {
            exitGroupDetail();
            await loadGroups();
        }
        
        // Internal UI state handling.
        await loadConversationsWithGroups();
    } catch (error) {
        console.error('DeleteminutesFailed:', error);
        alert('DeleteFailed: ' + (error.message || 'UnknownError'));
    }
}

// Internal UI state handling.
async function renameGroupFromContext() {
    const groupId = contextMenuGroupId;
    if (!groupId) return;

    try {
        const response = await apiFetch(`/api/groups/${groupId}`);
        const group = await response.json();
        if (!group) return;

        const renamePrompt = typeof window.t === 'function' ? window.t('chat.renameGroupPrompt') : 'Error:';
        const newName = prompt(renamePrompt, group.name);
        if (newName === null || !newName.trim()) {
            closeGroupContextMenu();
            return;
        }

        const trimmedName = newName.trim();
        
        // Internal UI state handling.
        let groups;
        if (Array.isArray(groupsCache) && groupsCache.length > 0) {
            groups = groupsCache;
        } else {
            const response = await apiFetch('/api/groups');
            groups = await response.json();
        }
        
        // Internal UI state handling.
        if (!Array.isArray(groups)) {
            groups = [];
        }
        
        const nameExists = groups.some(g => g.name === trimmedName && g.id !== groupId);
        if (nameExists) {
            alert(typeof window.t === 'function' ? window.t('createGroupModal.nameExists') : 'group, text');
            return;
        }

        const updateResponse = await apiFetch(`/api/groups/${groupId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                name: trimmedName,
                icon: group.icon || '📁',
            }),
        });

        if (!updateResponse.ok) {
            const error = await updateResponse.json();
            if (error.error && error.error.includes('Copied')) {
                alert('group, text');
                return;
            }
            throw new Error(error.error || 'UpdatedFailed');
        }

        loadGroups();
        
        // Internal UI state handling.
        if (currentGroupId === groupId) {
            const titleEl = document.getElementById('group-detail-title');
            if (titleEl) {
                titleEl.textContent = trimmedName;
            }
        }
    } catch (error) {
        console.error('Risk scoreFailed:', error);
        const failedLabel = typeof window.t === 'function' ? window.t('chat.renameFailed') : 'Failed';
        const unknownErr = typeof window.t === 'function' ? window.t('createGroupModal.unknownError') : 'UnknownError';
        alert(failedLabel + ': ' + (error.message || unknownErr));
    }

    closeGroupContextMenu();
}

// Internal UI state handling.
async function pinGroupFromContext() {
    const groupId = contextMenuGroupId;
    if (!groupId) return;

    try {
        // Internal UI state handling.
        const response = await apiFetch(`/api/groups/${groupId}`);
        const group = await response.json();
        if (!group) return;

        const newPinnedState = !group.pinned;

        // Internal UI state handling.
        const updateResponse = await apiFetch(`/api/groups/${groupId}/pinned`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                pinned: newPinnedState,
            }),
        });

        if (!updateResponse.ok) {
            const error = await updateResponse.json();
            throw new Error(error.error || 'UpdatedFailed');
        }

        // Internal UI state handling.
        loadGroups();
    } catch (error) {
        console.error('Risk scoreFailed:', error);
        alert('Failed: ' + (error.message || 'UnknownError'));
    }

    closeGroupContextMenu();
}

// Internal UI state handling.
async function deleteGroupFromContext() {
    const groupId = contextMenuGroupId;
    if (!groupId) return;

    const deleteConfirmMsg = typeof window.t === 'function' ? window.t('chat.deleteGroupConfirm') : 'Confirm Deletegroup?minutesChatDelete, group.';
    if (!confirm(deleteConfirmMsg)) {
        closeGroupContextMenu();
        return;
    }

    try {
        await apiFetch(`/api/groups/${groupId}`, {
            method: 'DELETE',
        });

        // Internal UI state handling.
        groupsCache = groupsCache.filter(g => g.id !== groupId);
        Object.keys(conversationGroupMappingCache).forEach(convId => {
            if (conversationGroupMappingCache[convId] === groupId) {
                delete conversationGroupMappingCache[convId];
            }
        });

        // Internal UI state handling.
        const submenu = document.getElementById('move-to-group-submenu');
        if (submenu && submenu.style.display !== 'none') {
            // Internal UI state handling.
            await loadGroups();
            await showMoveToGroupSubmenu();
        } else {
            // Internal UI state handling.
            if (currentGroupId === groupId) {
                exitGroupDetail();
            }
            await loadGroups();
        }
        
        // Internal UI state handling.
        await loadConversationsWithGroups();
    } catch (error) {
        console.error('DeleteminutesFailed:', error);
        alert('DeleteFailed: ' + (error.message || 'UnknownError'));
    }

    closeGroupContextMenu();
}

// Internal UI state handling.
function closeGroupContextMenu() {
    const menu = document.getElementById('group-context-menu');
    if (menu) {
        menu.style.display = 'none';
    }
    contextMenuGroupId = null;
}


// Internal UI state handling.
let groupSearchTimer = null;
let currentGroupSearchQuery = '';

// Internal UI state handling.
function toggleGroupSearch() {
    const searchContainer = document.getElementById('group-search-container');
    const searchInput = document.getElementById('group-search-input');
    
    if (!searchContainer || !searchInput) return;
    
    if (searchContainer.style.display === 'none') {
        searchContainer.style.display = 'block';
        searchInput.focus();
    } else {
        searchContainer.style.display = 'none';
        clearGroupSearch();
    }
}

// Internal UI state handling.
function handleGroupSearchInput(event) {
    // Internal UI state handling.
    if (event.key === 'Enter') {
        event.preventDefault();
        performGroupSearch();
        return;
    }
    
    // Internal UI state handling.
    if (event.key === 'Escape') {
        clearGroupSearch();
        toggleGroupSearch();
        return;
    }
    
    const searchInput = document.getElementById('group-search-input');
    const clearBtn = document.getElementById('group-search-clear-btn');
    
    if (!searchInput) return;
    
    const query = searchInput.value.trim();
    
    // Internal UI state handling.
    if (clearBtn) {
        clearBtn.style.display = query ? 'block' : 'none';
    }
    
    // Internal UI state handling.
    if (groupSearchTimer) {
        clearTimeout(groupSearchTimer);
    }
    
    groupSearchTimer = setTimeout(() => {
        performGroupSearch();
    }, 300); // Internal UI state handling.
}

// Internal UI state handling.
async function performGroupSearch() {
    const searchInput = document.getElementById('group-search-input');
    if (!searchInput || !currentGroupId) return;
    
    const query = searchInput.value.trim();
    currentGroupSearchQuery = query;
    
    // Internal UI state handling.
    await loadGroupConversations(currentGroupId, query);
}

// Internal UI state handling.
function clearGroupSearch() {
    const searchInput = document.getElementById('group-search-input');
    const clearBtn = document.getElementById('group-search-clear-btn');
    
    if (searchInput) {
        searchInput.value = '';
    }
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    
    currentGroupSearchQuery = '';
    
    // Internal UI state handling.
    if (currentGroupId) {
        loadGroupConversations(currentGroupId, '');
    }
}

// Internal UI state handling.
document.addEventListener('DOMContentLoaded', async () => {
    await loadGroups();
    await loadConversationsWithGroups();
    
    // Internal UI state handling.
    // Internal UI state handling.
    let lastFocusTime = Date.now();
    const CONVERSATION_REFRESH_INTERVAL = 30000; // Internal UI state handling.
    
    window.addEventListener('focus', () => {
        const now = Date.now();
        // Internal UI state handling.
        if (now - lastFocusTime > CONVERSATION_REFRESH_INTERVAL) {
            lastFocusTime = now;
            if (typeof loadConversationsWithGroups === 'function') {
                loadConversationsWithGroups();
            }
        }
    });
    
    // Internal UI state handling.
    document.addEventListener('visibilitychange', () => {
        if (!document.hidden) {
            // Internal UI state handling.
            const now = Date.now();
            if (now - lastFocusTime > CONVERSATION_REFRESH_INTERVAL) {
                lastFocusTime = now;
                if (typeof loadConversationsWithGroups === 'function') {
                    loadConversationsWithGroups();
                }
            }
        }
    });

    // Internal UI state handling.
    document.addEventListener('conversation-deleted', (e) => {
        const id = e.detail && e.detail.conversationId;
        if (!id) return;
        if (id === currentConversationId) {
            currentConversationId = null;
            try {
                window.currentConversationId = '';
            } catch (e) { /* ignore */ }
            const messagesDiv = document.getElementById('chat-messages');
            if (messagesDiv) messagesDiv.innerHTML = '';
            const readyMsg = typeof window.t === 'function' ? window.t('chat.systemReadyMessage') : 'System is ready. Automatic execution safety checks are enabled.';
            addMessage('assistant', readyMsg, null, null, null, { systemReadyMessage: true });
            addAttackChainButton(null);
        }
        if (typeof loadConversationsWithGroups === 'function') {
            loadConversationsWithGroups();
        } else if (typeof loadConversations === 'function') {
            loadConversations();
        }
    });
});

// Internal UI state handling.
if (typeof window !== 'undefined') {
    window.loadConversation = loadConversation;
    window.startNewConversation = startNewConversation;
}
