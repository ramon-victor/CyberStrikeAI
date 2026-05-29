// Role management functions
function _t(key, opts) {
    if (typeof window.t === 'function') {
        try {
            var translated = window.t(key, opts);
            if (typeof translated === 'string' && translated && translated !== key) {
                return translated;
            }
        } catch (e) { /* ignore */ }
    }
    // Avoid exposing keys to users when i18n is not ready or entries are missing (consistent with zh-CN defaults)
    if (key === 'roles.noDescription') return 'No description';
    if (key === 'roles.noDescriptionShort') return 'No description';
    if (key === 'roles.defaultRoleDescription') {
        return 'Default role; carries no additional user prompt and uses the default MCP';
    }
    return key;
}

/** Determine whether the current language is English */
function _isEnLang() {
    const locale = (typeof window.__locale === 'string' ? window.__locale : '') ||
        (typeof window.i18next !== 'undefined' ? (window.i18next.language || '') : '');
    return locale.toLowerCase().startsWith('en');
}

/** Get the role display name (based on current language) */
function getRoleDisplayName(role) {
    if (_isEnLang() && role.name_en) return role.name_en;
    return role.name || '';
}

/** Description in role config:trim,and treat literal i18n keys stored by mistake as empty */
function rolePlainDescription(role) {
    const raw = _isEnLang() && role.description_en
        ? role.description_en.trim()
        : (typeof role.description === 'string' ? role.description.trim() : '');
    if (!raw) return '';
    if (raw === 'roles.noDescription' || raw === 'roles.noDescriptionShort') return '';
    return raw;
}
let currentRole = localStorage.getItem('currentRole') || '';
const DEFAULT_ROLE_NAME = '\u9ed8\u8ba4';
let roles = [];
let rolesSearchKeyword = ''; // role search keyword
let rolesSearchTimeout = null; // search debounce timer
let allRoleTools = []; // Store all tool list (for role tool selection)
// Share localStorage with MCP tool config,to keep operations consistent
function getRoleToolsPageSize() {
    const saved = localStorage.getItem('toolsPageSize');
    const n = saved ? parseInt(saved, 10) : 20;
    return isNaN(n) || n < 1 ? 20 : n;
}
// Current role association filter: '' = all, 'role_on' = associated with this role, 'role_off' = not associated with this role
let roleToolsStatusFilter = '';
/** Cache the full list when filtering by role association (matching the current search),avoidlosing state across pages */
let roleToolsListCacheFull = [];
let roleToolsListCacheSearch = '';
/** Whether to use client-side pagination (true in role association filter mode) */
let roleToolsClientMode = false;
let roleToolsPagination = {
    page: 1,
    pageSize: getRoleToolsPageSize(),
    total: 0,
    totalPages: 1
};
let roleToolsSearchKeyword = ''; // tool search keyword
let roleToolStateMap = new Map(); // tool state map:toolKey -> { enabled: boolean, ... }
let roleUsesAllTools = false; // Mark whether the role uses all tools (when tools is not configured)
let totalEnabledToolsInMCP = 0; // total enabled tool count (from MCP management,from API response)
// Only update from unfiltered, unsearched request results for the stats denominator to avoid totals shrinking after filters.
let roleToolsStatsGrandTotal = 0; // total tool count (consistent with MCP list "all")
let roleToolsStatsMcpEnabledTotal = 0; // global MCP enabled tool count
let roleConfiguredTools = new Set(); // role configured tool list (used to determine which tools should be selected)

// Sort role list:default rolefirst,others by name
function sortRoles(rolesArray) {
    const sortedRoles = [...rolesArray];
    // Separate the default role
    const defaultRole = sortedRoles.find(r => r.name === DEFAULT_ROLE_NAME);
    const otherRoles = sortedRoles.filter(r => r.name !== DEFAULT_ROLE_NAME);
    
    // Sort other roles by name,keep stable order
    otherRoles.sort((a, b) => {
        const nameA = a.name || '';
        const nameB = b.name || '';
        return nameA.localeCompare(nameB, 'zh-CN');
    });
    
    // Place the default role first, then append other roles in sorted order
    const result = defaultRole ? [defaultRole, ...otherRoles] : otherRoles;
    return result;
}

// Load all roles
async function loadRoles() {
    if (window.i18nReady && typeof window.i18nReady.then === 'function') {
        try {
            await window.i18nReady;
        } catch (e) { /* ignore */ }
    }
    try {
        const response = await apiFetch('/api/roles');
        if (!response.ok) {
            throw new Error('Failed to load roles');
        }
        const data = await response.json();
        roles = data.roles || [];
        updateRoleSelectorDisplay();
        renderRoleSelectionSidebar(); // Render sidebar role list
        return roles;
    } catch (error) {
        console.error('Failed to load roles:', error);
        // Use i18n for notice text; if i18n is not initialized yet, fall back to readable English instead of exposing the key.
        var loadFailedLabel = (typeof window !== 'undefined' && typeof window.t === 'function')
            ? window.t('roles.loadFailed')
            : 'Failed to load roles';
        showNotification(loadFailedLabel + ': ' + error.message, 'error');
        return [];
    }
}

// Handle role changes
function handleRoleChange(roleName) {
    const oldRole = currentRole;
    currentRole = roleName || '';
    localStorage.setItem('currentRole', currentRole);
    updateRoleSelectorDisplay();
    renderRoleSelectionSidebar(); // Update sidebar selected state
    
    // When the role changes, mark the tool list for reload if it was loaded.
    // Then the next @tools suggestion trigger reloads the tool list for the new role.
    if (oldRole !== currentRole && typeof window !== 'undefined') {
        // Set a flag to tell chat.js to reload the tool list
        window._mentionToolsRoleChanged = true;
    }
}

// Update role selector display
function updateRoleSelectorDisplay() {
    const roleSelectorBtn = document.getElementById('role-selector-btn');
    const roleSelectorIcon = document.getElementById('role-selector-icon');
    const roleSelectorText = document.getElementById('role-selector-text');
    
    if (!roleSelectorBtn || !roleSelectorIcon || !roleSelectorText) return;

    let selectedRole;
    if (currentRole && currentRole !== DEFAULT_ROLE_NAME) {
        selectedRole = roles.find(r => r.name === currentRole);
    } else {
        selectedRole = roles.find(r => r.name === DEFAULT_ROLE_NAME);
    }

    if (selectedRole) {
        // Use the configured icon, or the default icon if absent.
        let icon = selectedRole.icon || '🔵';
        // If the icon is a Unicode escape format (\U0001F3C6), convert it to emoji.
        if (icon && typeof icon === 'string') {
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // ifConversion failed,use the default icon
                    console.warn('Failed to convert icon Unicode escape:', icon, e);
                    icon = '🔵';
                }
            }
        }
        roleSelectorIcon.textContent = icon;
        const isDefaultRole = selectedRole.name === DEFAULT_ROLE_NAME || !selectedRole.name;
        const displayName = isDefaultRole && typeof window.t === 'function'
            ? window.t('chat.defaultRole') : (getRoleDisplayName(selectedRole) || (typeof window.t === 'function' ? window.t('chat.defaultRole') : 'Default'));
        // For non-default roles, avoid i18n data-i18n overwriting text as "Default"
        roleSelectorText.setAttribute('data-i18n-skip-text', isDefaultRole ? 'false' : 'true');
        roleSelectorText.textContent = displayName;
    } else {
        // default role
        roleSelectorText.setAttribute('data-i18n-skip-text', 'false');
        roleSelectorIcon.textContent = '🔵';
        roleSelectorText.textContent = typeof window.t === 'function' ? window.t('chat.defaultRole') : 'Default';
    }
}

// Render the main content role selection list
function renderRoleSelectionSidebar() {
    const roleList = document.getElementById('role-selection-list');
    if (!roleList) return;

    // Clear list
    roleList.innerHTML = '';

    // Get the icon from role config, or use the default icon if not configured.
    function getRoleIcon(role) {
        if (role.icon) {
            // If the icon is a Unicode escape format (\U0001F3C6), convert it to emoji.
            let icon = role.icon;
            // Check whether this is Unicode escape format (may include quotes)
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // ifConversion failed,use original value
                    console.warn('Failed to convert icon Unicode escape:', icon, e);
                }
            }
            return icon;
        }
        // If no icon is configured,generate default icon from the first character of the role name
        // use some common default icons
        return '👤';
    }
    
    // Sort roles: default role first,others by name
    const sortedRoles = sortRoles(roles);
    
    // Show only enabled roles
    const enabledSortedRoles = sortedRoles.filter(r => r.enabled !== false);
    
    enabledSortedRoles.forEach(role => {
        const isDefaultRole = role.name === DEFAULT_ROLE_NAME;
        const isSelected = isDefaultRole ? (currentRole === '' || currentRole === DEFAULT_ROLE_NAME) : (currentRole === role.name);
        const roleItem = document.createElement('div');
        roleItem.className = 'role-selection-item-main' + (isSelected ? ' selected' : '');
        roleItem.onclick = () => {
            selectRole(role.name);
            closeRoleSelectionPanel(); // close panel automatically after selection
        };
        const icon = getRoleIcon(role);
        
        // Handle default role description
        const plainDesc = rolePlainDescription(role);
        let description = plainDesc || _t('roles.noDescription');
        if (isDefaultRole && !plainDesc) {
            description = _t('roles.defaultRoleDescription');
        }
        
        roleItem.innerHTML = `
            <div class="role-selection-item-icon-main">${icon}</div>
            <div class="role-selection-item-content-main">
                <div class="role-selection-item-name-main">${escapeHtml(getRoleDisplayName(role))}</div>
                <div class="role-selection-item-description-main">${escapeHtml(description)}</div>
            </div>
            ${isSelected ? '<div class="role-selection-checkmark-main">✓</div>' : ''}
        `;
        roleList.appendChild(roleItem);
    });
}

// Select role
function selectRole(roleName) {
    // Map the default role to an empty string (means default role)
    if (roleName === DEFAULT_ROLE_NAME) {
        roleName = '';
    }
    handleRoleChange(roleName);
    renderRoleSelectionSidebar(); // Re-render to update selected state
}

function getChatRoleSelectorWrapper() {
    return document.getElementById('role-selector-wrapper')
        || document.getElementById('role-selector-btn')?.closest('.role-selector-wrapper:not(.project-selector-wrapper)');
}

function isRoleSelectionPanelOpen() {
    const panel = document.getElementById('role-selection-panel');
    if (!panel) return false;
    return panel.style.display !== 'none' && panel.style.display !== '';
}

// Toggle role selection panel visibility
function toggleRoleSelectionPanel() {
    const panel = document.getElementById('role-selection-panel');
    const roleSelectorBtn = document.getElementById('role-selector-btn');
    if (!panel) return;
    
    const isHidden = !isRoleSelectionPanelOpen();
    
    if (isHidden) {
        if (typeof closeAgentModePanel === 'function') {
            closeAgentModePanel();
        }
        if (typeof closeChatProjectPanel === 'function') {
            closeChatProjectPanel();
        }
        if (typeof closeChatReasoningPanel === 'function') {
            closeChatReasoningPanel();
        }
        renderRoleSelectionSidebar();
        panel.style.display = 'flex'; // useflex layout
        // Add visual feedback for open state
        if (roleSelectorBtn) {
            roleSelectorBtn.classList.add('active');
            roleSelectorBtn.setAttribute('aria-expanded', 'true');
        }
        
        // Check position after panel renders
        setTimeout(() => {
            const wrapper = getChatRoleSelectorWrapper();
            if (wrapper) {
                const rect = wrapper.getBoundingClientRect();
                const panelHeight = panel.offsetHeight || 400;
                const viewportHeight = window.innerHeight;
                
                // ifpanel top is outside the viewport,scroll to a suitable position
                if (rect.top - panelHeight < 0) {
                    const scrollY = window.scrollY + rect.top - panelHeight - 20;
                    window.scrollTo({ top: Math.max(0, scrollY), behavior: 'smooth' });
                }
            }
        }, 10);
    } else {
        closeRoleSelectionPanel();
    }
}

// Close role selection panel (called automatically after selecting a role)
function closeRoleSelectionPanel() {
    const panel = document.getElementById('role-selection-panel');
    const roleSelectorBtn = document.getElementById('role-selector-btn');
    if (panel) {
        panel.style.display = 'none';
    }
    if (roleSelectorBtn) {
        roleSelectorBtn.classList.remove('active');
        roleSelectorBtn.setAttribute('aria-expanded', 'false');
    }
}

// Escape HTML
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Refresh role list
async function refreshRoles() {
    await loadRoles();
    // Check whether current page is role management page
    const currentPage = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || 'chat');
    if (currentPage === 'roles-management') {
        renderRolesList();
    }
    // Always update sidebar role selection list
    renderRoleSelectionSidebar();
    showNotification('Refreshed', 'success');
}

// Render role list
function renderRolesList() {
    const rolesList = document.getElementById('roles-list');
    if (!rolesList) return;

    // Filter roles (by search keyword)
    let filteredRoles = roles;
    if (rolesSearchKeyword) {
        const keyword = rolesSearchKeyword.toLowerCase();
        filteredRoles = roles.filter(role => 
            role.name.toLowerCase().includes(keyword) ||
            (role.description && role.description.toLowerCase().includes(keyword))
        );
    }

    if (filteredRoles.length === 0) {
        rolesList.innerHTML = '<div class="empty-state">' + 
            (rolesSearchKeyword ? _t('roles.noMatchingRoles') : _t('roles.noRoles')) + 
            '</div>';
        return;
    }

    // Sort roles: default role first,others by name
    const sortedRoles = sortRoles(filteredRoles);
    
    rolesList.innerHTML = sortedRoles.map(role => {
        const plainDesc = rolePlainDescription(role);
        // Get role icon; convert it to emoji if it is a Unicode escape format.
        let roleIcon = role.icon || '👤';
        if (roleIcon && typeof roleIcon === 'string') {
            // Check whether this is Unicode escape format (may include quotes)
            const unicodeMatch = roleIcon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    roleIcon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // ifConversion failed,use the default icon
                    console.warn('Failed to convert icon Unicode escape:', roleIcon, e);
                    roleIcon = '👤';
                }
            }
        }

        // Get tool list display
        let toolsDisplay = '';
        let toolsCount = 0;
        if (role.name === DEFAULT_ROLE_NAME) {
            toolsDisplay = _t('roleModal.usingAllTools');
        } else if (role.tools && role.tools.length > 0) {
            toolsCount = role.tools.length;
            // Show the first 5 tool names
            const toolNames = role.tools.slice(0, 5).map(tool => {
                // If this is an external tool, the format is external_mcp::tool_name; show only the tool name.
                const toolName = tool.includes('::') ? tool.split('::')[1] : tool;
                return escapeHtml(toolName);
            });
            if (toolsCount <= 5) {
                toolsDisplay = toolNames.join(', ');
            } else {
                toolsDisplay = toolNames.join(', ') + _t('roleModal.andNMore', { count: toolsCount });
            }
        } else if (role.mcps && role.mcps.length > 0) {
            toolsCount = role.mcps.length;
            toolsDisplay = _t('roleModal.andNMore', { count: toolsCount });
        } else {
            toolsDisplay = _t('roleModal.usingAllTools');
        }

        return `
        <div class="role-card">
            <div class="role-card-header">
                <h3 class="role-card-title">
                    <span class="role-card-icon">${roleIcon}</span>
                    ${escapeHtml(role.name)}
                </h3>
                <span class="role-card-badge ${role.enabled !== false ? 'enabled' : 'disabled'}">
                    ${role.enabled !== false ? _t('roles.enabled') : _t('roles.disabled')}
                </span>
            </div>
            <div class="role-card-description">${escapeHtml(plainDesc || _t('roles.noDescriptionShort'))}</div>
            <div class="role-card-tools">
                <span class="role-card-tools-label">${_t('roleModal.toolsLabel')}</span>
                <span class="role-card-tools-value">${toolsDisplay}</span>
            </div>
            <div class="role-card-actions">
                <button class="btn-secondary btn-small" onclick="editRole('${escapeHtml(role.name)}')">${_t('common.edit')}</button>
                ${role.name !== DEFAULT_ROLE_NAME ? `<button class="btn-secondary btn-small btn-danger" onclick="deleteRole('${escapeHtml(role.name)}')">${_t('common.delete')}</button>` : ''}
            </div>
        </div>
    `;
    }).join('');
}

// handlerolesearchinput
function handleRolesSearchInput() {
    clearTimeout(rolesSearchTimeout);
    rolesSearchTimeout = setTimeout(() => {
        searchRoles();
    }, 300);
}

// searchrole
function searchRoles() {
    const searchInput = document.getElementById('roles-search');
    if (!searchInput) return;
    
    rolesSearchKeyword = searchInput.value.trim();
    const clearBtn = document.getElementById('roles-search-clear');
    if (clearBtn) {
        clearBtn.style.display = rolesSearchKeyword ? 'block' : 'none';
    }
    
    renderRolesList();
}

// clearrolesearch
function clearRolesSearch() {
    const searchInput = document.getElementById('roles-search');
    if (searchInput) {
        searchInput.value = '';
    }
    rolesSearchKeyword = '';
    const clearBtn = document.getElementById('roles-search-clear');
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    renderRolesList();
}

// Generate unique tool identifier (consistent with getToolKey in settings.js)
function getToolKey(tool) {
    // If this is an external tool,use external_mcp::tool.name as unique identifier
    if (tool.is_external && tool.external_mcp) {
        return `${tool.external_mcp}::${tool.name}`;
    }
    // Built-in tools use tool names directly
    return tool.name;
}

// Merge one tool into roleToolStateMap (consistent with single-item logic in loadRoleTools)
function mergeToolIntoRoleStateMap(tool) {
    const toolKey = getToolKey(tool);
    if (!roleToolStateMap.has(toolKey)) {
        let enabled = false;
        if (roleUsesAllTools) {
            enabled = tool.enabled ? true : false;
        } else {
            enabled = roleConfiguredTools.has(toolKey);
        }
        roleToolStateMap.set(toolKey, {
            enabled: enabled,
            is_external: tool.is_external || false,
            external_mcp: tool.external_mcp || '',
            name: tool.name,
            mcpEnabled: tool.enabled
        });
    } else {
        const state = roleToolStateMap.get(toolKey);
        if (roleUsesAllTools && tool.enabled) {
            state.enabled = true;
        }
        state.is_external = tool.is_external || false;
        state.external_mcp = tool.external_mcp || '';
        state.mcpEnabled = tool.enabled;
        if (!state.name || state.name === toolKey.split('::').pop()) {
            state.name = tool.name;
        }
    }
}

function getRoleLinkedForTool(toolKey, tool) {
    if (roleToolStateMap.has(toolKey)) {
        return !!roleToolStateMap.get(toolKey).enabled;
    }
    if (roleUsesAllTools) {
        return tool.enabled !== false;
    }
    return roleConfiguredTools.has(toolKey);
}

function computeRoleLinkFilteredTools() {
    if (!roleToolsListCacheFull.length) {
        return [];
    }
    return roleToolsListCacheFull.filter(tool => {
        const key = getToolKey(tool);
        const linked = getRoleLinkedForTool(key, tool);
        if (roleToolsStatusFilter === 'role_on') {
            return linked;
        }
        if (roleToolsStatusFilter === 'role_off') {
            return !linked;
        }
        return true;
    });
}

async function fetchAllRoleToolsIntoCache(searchKeyword) {
    const pageSize = 100;
    let page = 1;
    const all = [];
    let totalPages = 1;
    do {
        let url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
        if (searchKeyword) {
            url += `&search=${encodeURIComponent(searchKeyword)}`;
        }
        const response = await apiFetch(url);
        if (!response.ok) {
            throw new Error('Failed to get tool list');
        }
        const result = await response.json();
        const tools = result.tools || [];
        tools.forEach(tool => mergeToolIntoRoleStateMap(tool));
        all.push(...tools);
        totalPages = Math.max(1, result.total_pages || 1);
        page++;
    } while (page <= totalPages);
    roleToolsListCacheFull = all;
    roleToolsStatsGrandTotal = all.length;
    roleToolsStatsMcpEnabledTotal = all.filter(t => t.enabled !== false).length;
    totalEnabledToolsInMCP = roleToolsStatsMcpEnabledTotal;
}

// Save current page tool states to the global map
function saveCurrentRolePageToolStates() {
    document.querySelectorAll('#role-tools-list .role-tool-item').forEach(item => {
        const toolKey = item.dataset.toolKey;
        const checkbox = item.querySelector('input[type="checkbox"]');
        if (toolKey && checkbox) {
            const toolName = item.dataset.toolName;
            const isExternal = item.dataset.isExternal === 'true';
            const externalMcp = item.dataset.externalMcp || '';
            const existingState = roleToolStateMap.get(toolKey);
            roleToolStateMap.set(toolKey, {
                enabled: checkbox.checked,
                is_external: isExternal,
                external_mcp: externalMcp,
                name: toolName,
                mcpEnabled: existingState ? existingState.mcpEnabled : true // Preserve MCP enabled state
            });
        }
    });
}

// Load all tool list (for role tool selection)
async function loadRoleTools(page = 1, searchKeyword = '') {
    try {
        // Before loading a new page,save current page state to the global map first
        saveCurrentRolePageToolStates();

        const pageSize = roleToolsPagination.pageSize;
        const needRoleLinkFilter =
            roleToolsStatusFilter === 'role_on' || roleToolsStatusFilter === 'role_off';

        if (needRoleLinkFilter) {
            roleToolsClientMode = true;
            const searchChanged = searchKeyword !== roleToolsListCacheSearch;
            if (searchChanged || roleToolsListCacheFull.length === 0) {
                await fetchAllRoleToolsIntoCache(searchKeyword);
                roleToolsListCacheSearch = searchKeyword;
            }
            const filtered = computeRoleLinkFilteredTools();
            const total = filtered.length;
            let totalPages = Math.max(1, Math.ceil(total / pageSize) || 1);
            let p = page;
            if (p > totalPages) {
                p = totalPages;
            }
            if (p < 1) {
                p = 1;
            }
            roleToolsPagination = {
                page: p,
                pageSize,
                total,
                totalPages
            };
            allRoleTools = filtered.slice((p - 1) * pageSize, p * pageSize);
        } else {
            roleToolsClientMode = false;
            roleToolsListCacheFull = [];
            roleToolsListCacheSearch = '';

            let url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
            if (searchKeyword) {
                url += `&search=${encodeURIComponent(searchKeyword)}`;
            }

            const response = await apiFetch(url);
            if (!response.ok) {
                throw new Error('Failed to get tool list');
            }

            const result = await response.json();
            allRoleTools = result.tools || [];
            roleToolsPagination = {
                page: result.page || page,
                pageSize: result.page_size || pageSize,
                total: result.total || 0,
                totalPages: result.total_pages || 1
            };

            if (roleToolsStatusFilter === '' && !searchKeyword) {
                roleToolsStatsGrandTotal = result.total || 0;
                if (result.total_enabled !== undefined) {
                    roleToolsStatsMcpEnabledTotal = result.total_enabled;
                    totalEnabledToolsInMCP = result.total_enabled;
                }
            }

            allRoleTools.forEach(tool => mergeToolIntoRoleStateMap(tool));
        }

        renderRoleToolsList();
        renderRoleToolsPagination();
        updateRoleToolsStats();
    } catch (error) {
        console.error('Failed to load tool list:', error);
        const toolsList = document.getElementById('role-tools-list');
        if (toolsList) {
            toolsList.innerHTML = `<div class="tools-error">${_t('roleModal.loadToolsFailed')}: ${escapeHtml(error.message)}</div>`;
        }
    }
}

// Render role tool selection list
function renderRoleToolsList() {
    const toolsList = document.getElementById('role-tools-list');
    if (!toolsList) return;
    
    // Clear loading prompt and old content
    toolsList.innerHTML = '';

    if (roleToolsStatusFilter === 'role_on') {
        const banner = document.createElement('div');
        banner.className = 'role-tools-filter-banner role-tools-filter-banner-on';
        banner.setAttribute('role', 'status');
        banner.textContent = _t('roleModal.roleFilterOnBanner');
        toolsList.appendChild(banner);
    } else if (roleToolsStatusFilter === 'role_off') {
        const banner = document.createElement('div');
        banner.className = 'role-tools-filter-banner role-tools-filter-banner-off';
        banner.setAttribute('role', 'status');
        banner.textContent = _t('roleModal.roleFilterOffBanner');
        toolsList.appendChild(banner);
    }
    
    const listContainer = document.createElement('div');
    listContainer.className = 'role-tools-list-items';
    listContainer.innerHTML = '';
    
    if (allRoleTools.length === 0) {
        listContainer.innerHTML = '<div class="tools-empty">' + _t('roleModal.noTools') + '</div>';
        toolsList.appendChild(listContainer);
        return;
    }

    const chkTitle = escapeHtml(_t('roleModal.checkboxLinkTitle'));
    
    allRoleTools.forEach(tool => {
        const toolKey = getToolKey(tool);
        const toolItem = document.createElement('div');
        toolItem.className = 'role-tool-item';
        toolItem.dataset.toolKey = toolKey;
        toolItem.dataset.toolName = tool.name;
        toolItem.dataset.isExternal = tool.is_external ? 'true' : 'false';
        toolItem.dataset.externalMcp = tool.external_mcp || '';
        
        // Get tool state from status map
        const toolState = roleToolStateMap.get(toolKey) || {
            enabled: tool.enabled,
            is_external: tool.is_external || false,
            external_mcp: tool.external_mcp || ''
        };
        
        // External tool badge
        let externalBadge = '';
        if (toolState.is_external || tool.is_external) {
            const externalMcpName = toolState.external_mcp || tool.external_mcp || '';
            const badgeText = externalMcpName ? `External (${escapeHtml(externalMcpName)})` : 'External';
            const badgeTitle = externalMcpName ? `External MCP tool - source: ${escapeHtml(externalMcpName)}` : 'External MCP tool';
            externalBadge = `<span class="external-tool-badge" title="${badgeTitle}">${badgeText}</span>`;
        }
        let mcpDisabledBadge = '';
        if (tool.enabled === false) {
            mcpDisabledBadge = `<span class="role-tool-mcp-disabled-badge" title="${escapeHtml(_t('roleModal.mcpDisabledBadgeTitle'))}">${escapeHtml(_t('roleModal.mcpDisabledBadge'))}</span>`;
        }
        // Generate unique checkbox id
        const checkboxId = `role-tool-${escapeHtml(toolKey).replace(/::/g, '--')}`;
        
        toolItem.innerHTML = `
            <input type="checkbox" id="${checkboxId}" ${toolState.enabled ? 'checked' : ''} 
                   title="${chkTitle}" aria-label="${chkTitle}"
                   onchange="handleRoleToolCheckboxChange('${escapeHtml(toolKey)}', this.checked)" />
            <div class="role-tool-item-info">
                <div class="role-tool-item-name">
                    ${escapeHtml(tool.name)}
                    ${externalBadge}
                    ${mcpDisabledBadge}
                </div>
                <div class="role-tool-item-desc">${escapeHtml(tool.description || 'No description')}</div>
            </div>
        `;
        listContainer.appendChild(toolItem);
    });
    
    toolsList.appendChild(listContainer);
}

// Render tool list pagination controls (always show range and page size,so page size can still be adjusted with only one page)
function renderRoleToolsPagination() {
    const toolsList = document.getElementById('role-tools-list');
    if (!toolsList) return;
    
    // Remove old pagination controls
    const oldPagination = toolsList.querySelector('.role-tools-pagination');
    if (oldPagination) {
        oldPagination.remove();
    }
    
    const pagination = document.createElement('div');
    pagination.className = 'role-tools-pagination';
    
    const { page, totalPages, total, pageSize } = roleToolsPagination;
    const startItem = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const endItem = total === 0 ? 0 : Math.min(page * pageSize, total);
    const savedPageSize = getRoleToolsPageSize();
    const perPageLabel = typeof window.t === 'function' ? window.t('mcp.perPage') : 'Per page';
    
    const paginationShowText = _t('roleModal.paginationShow', { start: startItem, end: endItem, total: total }) +
        (roleToolsSearchKeyword ? _t('roleModal.paginationSearch', { keyword: roleToolsSearchKeyword }) : '');
    const navDisabled = total === 0 || totalPages <= 1;
    pagination.innerHTML = `
        <div class="pagination-info">${paginationShowText}</div>
        <div class="pagination-page-size">
            <label for="role-tools-page-size-pagination">${escapeHtml(perPageLabel)}</label>
            <select id="role-tools-page-size-pagination" onchange="changeRoleToolsPageSize()">
                <option value="10" ${savedPageSize === 10 ? 'selected' : ''}>10</option>
                <option value="20" ${savedPageSize === 20 ? 'selected' : ''}>20</option>
                <option value="50" ${savedPageSize === 50 ? 'selected' : ''}>50</option>
                <option value="100" ${savedPageSize === 100 ? 'selected' : ''}>100</option>
            </select>
        </div>
        <div class="pagination-controls">
            <button class="btn-secondary" onclick="loadRoleTools(1, '${escapeHtml(roleToolsSearchKeyword)}')" ${page === 1 || navDisabled ? 'disabled' : ''}>${_t('roleModal.firstPage')}</button>
            <button class="btn-secondary" onclick="loadRoleTools(${page - 1}, '${escapeHtml(roleToolsSearchKeyword)}')" ${page === 1 || navDisabled ? 'disabled' : ''}>${_t('roleModal.prevPage')}</button>
            <span class="pagination-page">${_t('roleModal.pageOf', { page: page, total: totalPages })}</span>
            <button class="btn-secondary" onclick="loadRoleTools(${page + 1}, '${escapeHtml(roleToolsSearchKeyword)}')" ${page === totalPages || navDisabled ? 'disabled' : ''}>${_t('roleModal.nextPage')}</button>
            <button class="btn-secondary" onclick="loadRoleTools(${totalPages}, '${escapeHtml(roleToolsSearchKeyword)}')" ${page === totalPages || navDisabled ? 'disabled' : ''}>${_t('roleModal.lastPage')}</button>
        </div>
    `;
    
    toolsList.appendChild(pagination);
}

function syncRoleToolsFilterButtons() {
    const wrap = document.getElementById('role-tools-status-filter');
    if (!wrap) return;
    wrap.querySelectorAll('.btn-filter').forEach(btn => {
        const v = btn.getAttribute('data-filter');
        const filterVal = v === null || v === undefined ? '' : String(v);
        btn.classList.toggle('active', filterVal === roleToolsStatusFilter);
    });
}

function roleToolsListScopeLine() {
    const n = roleToolsPagination.total || 0;
    if (roleToolsStatusFilter === 'role_on') {
        return _t('roleModal.statsListScopeRoleOn', { n: n });
    }
    if (roleToolsStatusFilter === 'role_off') {
        return _t('roleModal.statsListScopeRoleOff', { n: n });
    }
    return _t('roleModal.statsListScopeAll', { n: n });
}

function filterRoleToolsByStatus(status) {
    roleToolsStatusFilter = status;
    syncRoleToolsFilterButtons();
    loadRoleTools(1, roleToolsSearchKeyword);
}

async function changeRoleToolsPageSize() {
    const sel = document.getElementById('role-tools-page-size-pagination');
    if (!sel) return;
    const newPageSize = parseInt(sel.value, 10);
    if (isNaN(newPageSize) || newPageSize < 1) return;
    localStorage.setItem('toolsPageSize', String(newPageSize));
    roleToolsPagination.pageSize = newPageSize;
    await loadRoleTools(1, roleToolsSearchKeyword);
}

// Handle tool checkbox state changes
function handleRoleToolCheckboxChange(toolKey, enabled) {
    const toolItem = document.querySelector(`.role-tool-item[data-tool-key="${toolKey}"]`);
    if (toolItem) {
        const toolName = toolItem.dataset.toolName;
        const isExternal = toolItem.dataset.isExternal === 'true';
        const externalMcp = toolItem.dataset.externalMcp || '';
        const existingState = roleToolStateMap.get(toolKey);
        roleToolStateMap.set(toolKey, {
            enabled: enabled,
            is_external: isExternal,
            external_mcp: externalMcp,
            name: toolName,
            mcpEnabled: existingState ? existingState.mcpEnabled : true // Preserve MCP enabled state
        });
    }
    if (
        roleToolsClientMode &&
        (roleToolsStatusFilter === 'role_on' || roleToolsStatusFilter === 'role_off')
    ) {
        loadRoleTools(roleToolsPagination.page, roleToolsSearchKeyword);
    } else {
        updateRoleToolsStats();
    }
}

// Select all tools
function selectAllRoleTools() {
    document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
        const toolItem = checkbox.closest('.role-tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const toolName = toolItem.dataset.toolName;
            const isExternal = toolItem.dataset.isExternal === 'true';
            const externalMcp = toolItem.dataset.externalMcp || '';
            if (toolKey) {
                const existingState = roleToolStateMap.get(toolKey);
                // Select only tools enabled in MCP management.
                const shouldEnable = existingState && existingState.mcpEnabled !== false;
                checkbox.checked = shouldEnable;
                roleToolStateMap.set(toolKey, {
                    enabled: shouldEnable,
                    is_external: isExternal,
                    external_mcp: externalMcp,
                    name: toolName,
                    mcpEnabled: existingState ? existingState.mcpEnabled : true
                });
            }
        }
    });
    if (
        roleToolsClientMode &&
        (roleToolsStatusFilter === 'role_on' || roleToolsStatusFilter === 'role_off')
    ) {
        loadRoleTools(roleToolsPagination.page, roleToolsSearchKeyword);
    } else {
        updateRoleToolsStats();
    }
}

// Deselect all tools
function deselectAllRoleTools() {
    document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
        checkbox.checked = false;
        const toolItem = checkbox.closest('.role-tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const toolName = toolItem.dataset.toolName;
            const isExternal = toolItem.dataset.isExternal === 'true';
            const externalMcp = toolItem.dataset.externalMcp || '';
            if (toolKey) {
                const existingState = roleToolStateMap.get(toolKey);
                roleToolStateMap.set(toolKey, {
                    enabled: false,
                    is_external: isExternal,
                    external_mcp: externalMcp,
                    name: toolName,
                    mcpEnabled: existingState ? existingState.mcpEnabled : true // Preserve MCP enabled state
                });
            }
        }
    });
    if (
        roleToolsClientMode &&
        (roleToolsStatusFilter === 'role_on' || roleToolsStatusFilter === 'role_off')
    ) {
        loadRoleTools(roleToolsPagination.page, roleToolsSearchKeyword);
    } else {
        updateRoleToolsStats();
    }
}

// Search tools
function searchRoleTools(keyword) {
    roleToolsSearchKeyword = keyword;
    const clearBtn = document.getElementById('role-tools-search-clear');
    if (clearBtn) {
        clearBtn.style.display = keyword ? 'block' : 'none';
    }
    loadRoleTools(1, keyword);
}

// Clear search
function clearRoleToolsSearch() {
    document.getElementById('role-tools-search').value = '';
    searchRoleTools('');
}

// Update tool statistics (definition:denominator"association cap"= all MCP enabled tool count,and MCP management page filter"MCP enabled"count consistent;checked=associated with this role)
function updateRoleToolsStats() {
    const statsEl = document.getElementById('role-tools-stats');
    if (!statsEl) return;

    const pageChecked = Array.from(document.querySelectorAll('#role-tools-list input[type="checkbox"]:checked')).length;
    const pageTotal = document.querySelectorAll('#role-tools-list input[type="checkbox"]').length;
    const mcpOnMax =
        (roleToolsStatsMcpEnabledTotal > 0 ? roleToolsStatsMcpEnabledTotal : totalEnabledToolsInMCP) || 0;
    const grandAll =
        (roleToolsStatsGrandTotal > 0 ? roleToolsStatsGrandTotal : roleToolsPagination.total) || 0;
    const scopeLine = roleToolsListScopeLine();

    if (roleUsesAllTools) {
        statsEl.innerHTML = `
            <div class="role-tools-stats-row">
                <span title="${escapeHtml(_t('roleModal.statsPageLinkedTitle'))}">✅ ${_t('roleModal.statsPageLinked', { current: pageChecked, total: pageTotal })}</span>
            </div>
            <div class="role-tools-stats-row">
                <span title="${escapeHtml(_t('roleModal.statsRoleUsesAllTitle'))}">📊 ${_t('roleModal.statsRoleUsesAll', { mcpOn: mcpOnMax, all: grandAll })}</span>
            </div>
            <div class="role-tools-stats-hint">📋 ${escapeHtml(scopeLine)}</div>
        `;
        return;
    }

    let roleLinked = 0;
    roleToolStateMap.forEach(state => {
        if (state.enabled && state.mcpEnabled !== false) {
            roleLinked++;
        }
    });
    document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
        const toolItem = checkbox.closest('.role-tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const savedState = roleToolStateMap.get(toolKey);
            if (savedState && savedState.enabled !== checkbox.checked && savedState.mcpEnabled !== false) {
                if (checkbox.checked && !savedState.enabled) {
                    roleLinked++;
                } else if (!checkbox.checked && savedState.enabled) {
                    roleLinked--;
                }
            }
        }
    });

    const roleRow =
        mcpOnMax > 0
            ? `<span title="${escapeHtml(_t('roleModal.statsRoleLinkedTitle'))}">📊 ${_t('roleModal.statsRoleLinked', { current: roleLinked, max: mcpOnMax })}</span>`
            : `<span title="${escapeHtml(_t('roleModal.statsRoleLinkedNoMaxTitle'))}">📊 ${_t('roleModal.statsRoleLinkedNoMax', { current: roleLinked })}</span>`;

    statsEl.innerHTML = `
        <div class="role-tools-stats-row">
            <span title="${escapeHtml(_t('roleModal.statsPageLinkedTitle'))}">✅ ${_t('roleModal.statsPageLinked', { current: pageChecked, total: pageTotal })}</span>
        </div>
        <div class="role-tools-stats-row">${roleRow}</div>
        <div class="role-tools-stats-hint">📋 ${escapeHtml(scopeLine)}</div>
    `;
}

// Get selected tool list (Return toolKey array)
async function getSelectedRoleTools() {
    // Save current page state.
    saveCurrentRolePageToolStates();
    
    // ifthere is no search keyword,need to load tools from all pages to ensure the status map is complete
    // but for performance,we can get selected tools only from the status map
    // The issue is:ifuserselected tools only on some pages,tool states from other pages may not be in the map
    
    // iftotal tool count is greater than loaded tool count,we need to ensure all unloaded page tools are considered
    // But for role tool selection,we only need tools explicitly selected by the user
    // so getting selected tools directly from the status map is enough
    
    // Get all selected tools from the status map, returning only tools enabled in MCP management.
    const selectedTools = [];
    roleToolStateMap.forEach((state, toolKey) => {
        // Return only tools that are enabled in MCP management and selected by the role.
        if (state.enabled && state.mcpEnabled !== false) {
            selectedTools.push(toolKey);
        }
    });
    
    // ifusermay have selected tools on other pages,we need to ensurecurrent page state is also saved
    // butStatus mapshould already contain states for all visited pages
    
    return selectedTools;
}

// Set selected tools (for editing roles)
function setSelectedRoleTools(selectedToolKeys) {
    const selectedSet = new Set(selectedToolKeys || []);
    
    // Update status map
    roleToolStateMap.forEach((state, toolKey) => {
        state.enabled = selectedSet.has(toolKey);
    });
    
    // Update current page checkbox state
    document.querySelectorAll('#role-tools-list .role-tool-item').forEach(item => {
        const toolKey = item.dataset.toolKey;
        const checkbox = item.querySelector('input[type="checkbox"]');
        if (toolKey && checkbox) {
            checkbox.checked = selectedSet.has(toolKey);
        }
    });
    
    updateRoleToolsStats();
}

// Show add role modal
async function showAddRoleModal() {
    const modal = document.getElementById('role-modal');
    if (!modal) return;

    document.getElementById('role-modal-title').textContent = _t('roleModal.addRole');
    document.getElementById('role-name').value = '';
    document.getElementById('role-name').disabled = false;
    document.getElementById('role-description').value = '';
    document.getElementById('role-icon').value = '';
    document.getElementById('role-user-prompt').value = '';
    document.getElementById('role-enabled').checked = true;

    // when adding role:show tool selection UI,hide default role hint
    const toolsSection = document.getElementById('role-tools-section');
    const defaultHint = document.getElementById('role-tools-default-hint');
    const toolsControls = document.querySelector('.role-tools-controls');
    const toolsList = document.getElementById('role-tools-list');
    const formHint = toolsSection ? toolsSection.querySelector('.form-hint') : null;
    
    if (defaultHint) {
        defaultHint.style.display = 'none';
    }
    if (toolsControls) {
        toolsControls.style.display = 'block';
    }
    if (toolsList) {
        toolsList.style.display = 'block';
    }
    if (formHint) {
        formHint.style.display = 'block';
    }

    // Reset tool state
    roleToolStateMap.clear();
    roleConfiguredTools.clear(); // Clear role configured tools list
    roleUsesAllTools = false; // When adding a role, default does not use all tools
    roleToolsSearchKeyword = '';
    const searchInput = document.getElementById('role-tools-search');
    if (searchInput) {
        searchInput.value = '';
    }
    const clearBtn = document.getElementById('role-tools-search-clear');
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    roleToolsStatusFilter = '';
    syncRoleToolsFilterButtons();
    roleToolsPagination.pageSize = getRoleToolsPageSize();
    
    // Clear tool list DOM,avoid loadRoleTools saveCurrentRolePageToolStates reading old state
    if (toolsList) {
        toolsList.innerHTML = '';
    }

    // Load and render tool list
    await loadRoleTools(1, '');
    
    // Ensure tool list is visible
    if (toolsList) {
        toolsList.style.display = 'block';
    }
    
    // Ensure statistics update correctly (show 0/108)
    updateRoleToolsStats();

    modal.style.display = 'flex';
}

// Edit role
async function editRole(roleName) {
    const role = roles.find(r => r.name === roleName);
    if (!role) {
        showNotification(_t('roleModal.roleNotFound'), 'error');
        return;
    }

    const modal = document.getElementById('role-modal');
    if (!modal) return;

    document.getElementById('role-modal-title').textContent = _t('roleModal.editRole');
    document.getElementById('role-name').value = role.name;
    document.getElementById('role-name').disabled = true; // Name cannot be changed while editing
    document.getElementById('role-description').value = role.description || '';
    // Handle icon field:ifisUnicodeescapeformat,convert toemoji;otherwise use directly
    let iconValue = role.icon || '';
    if (iconValue && iconValue.startsWith('\\U')) {
        // Convert Unicode escape format (such as \U0001F3C6) to emoji.
        try {
            const codePoint = parseInt(iconValue.substring(2), 16);
            iconValue = String.fromCodePoint(codePoint);
        } catch (e) {
            // ifConversion failed,use original value
        }
    }
    document.getElementById('role-icon').value = iconValue;
    document.getElementById('role-user-prompt').value = role.user_prompt || '';
    document.getElementById('role-enabled').checked = role.enabled !== false;

    // checkwhether this is the default role
    const isDefaultRole = roleName === DEFAULT_ROLE_NAME;
    const toolsSection = document.getElementById('role-tools-section');
    const defaultHint = document.getElementById('role-tools-default-hint');
    const toolsControls = document.querySelector('.role-tools-controls');
    const toolsList = document.getElementById('role-tools-list');
    const formHint = toolsSection ? toolsSection.querySelector('.form-hint') : null;
    
    if (isDefaultRole) {
        // default role:hide tool selection UI,show hint
        if (defaultHint) {
            defaultHint.style.display = 'block';
        }
        if (toolsControls) {
            toolsControls.style.display = 'none';
        }
        if (toolsList) {
            toolsList.style.display = 'none';
        }
        if (formHint) {
            formHint.style.display = 'none';
        }
    } else {
        // non-default role:show tool selection UI,hide hint
        if (defaultHint) {
            defaultHint.style.display = 'none';
        }
        if (toolsControls) {
            toolsControls.style.display = 'block';
        }
        if (toolsList) {
            toolsList.style.display = 'block';
        }
        if (formHint) {
            formHint.style.display = 'block';
        }

        // Reset tool state
        roleToolStateMap.clear();
        roleConfiguredTools.clear(); // Clear role configured tools list
        roleToolsSearchKeyword = '';
        const searchInput = document.getElementById('role-tools-search');
        if (searchInput) {
            searchInput.value = '';
        }
        const clearBtn = document.getElementById('role-tools-search-clear');
        if (clearBtn) {
            clearBtn.style.display = 'none';
        }
        roleToolsStatusFilter = '';
        syncRoleToolsFilterButtons();
        roleToolsPagination.pageSize = getRoleToolsPageSize();

        // Prefer the tools field; use mcps if tools is absent for backward compatibility.
        const selectedTools = role.tools || (role.mcps && role.mcps.length > 0 ? role.mcps : []);
        
        // Determine whether all tools are used:if tools are not configured (or tools is an empty array),means use all tools
        roleUsesAllTools = !role.tools || role.tools.length === 0;
        
        // Save role configured tools list
        if (selectedTools.length > 0) {
            selectedTools.forEach(toolKey => {
                roleConfiguredTools.add(toolKey);
            });
        }
        
        // If selected tools exist,initialize the status map first
        if (selectedTools.length > 0) {
            roleUsesAllTools = false; // tools configured,do not use all tools
            // Add selected tools to status map (mark selected)
            selectedTools.forEach(toolKey => {
                // ifthis tool is not in the map yet,Create firsta default state (enabled is true)
                if (!roleToolStateMap.has(toolKey)) {
                    roleToolStateMap.set(toolKey, {
                        enabled: true,
                        is_external: false,
                        external_mcp: '',
                        name: toolKey.split('::').pop() || toolKey // extract tool name from toolKey
                    });
                } else {
                    // ifalready exists,update to selected state
                    const state = roleToolStateMap.get(toolKey);
                    state.enabled = true;
                }
            });
        }

        // Load tool list (first page)
        await loadRoleTools(1, '');
        
        // ifuse all tools,mark all enabled tools on current page selected
        if (roleUsesAllTools) {
            // mark current pageallenabled in MCP management toolsasselected
            document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
                const toolItem = checkbox.closest('.role-tool-item');
                if (toolItem) {
                    const toolKey = toolItem.dataset.toolKey;
                    const toolName = toolItem.dataset.toolName;
                    const isExternal = toolItem.dataset.isExternal === 'true';
                    const externalMcp = toolItem.dataset.externalMcp || '';
                    if (toolKey) {
                        const state = roleToolStateMap.get(toolKey);
                        // Select only tools enabled in MCP management.
                        // ifstate exists,use value from state mcpEnabled;otherwise assume enabled (because loadRoleTools textshould already have initialized all tools)
                        const shouldEnable = state ? (state.mcpEnabled !== false) : true;
                        checkbox.checked = shouldEnable;
                        if (state) {
                            state.enabled = shouldEnable;
                        } else {
                            // ifstate does not exist,create new state (this should not happen,because loadRoleTools should already be initialized)
                            roleToolStateMap.set(toolKey, {
                                enabled: shouldEnable,
                                is_external: isExternal,
                                external_mcp: externalMcp,
                                name: toolName,
                                mcpEnabled: true // assume enabled,actual value updates in loadRoleTools
                            });
                        }
                    }
                }
            });
            // Update statistics,ensure correctselectedcount
            updateRoleToolsStats();
        } else if (selectedTools.length > 0) {
            // After loading completes,set selected state again (ensuretools on current page are also set correctly)
            setSelectedRoleTools(selectedTools);
        }
    }

    modal.style.display = 'flex';
}

// Close role modal
function closeRoleModal() {
    const modal = document.getElementById('role-modal');
    if (modal) {
        modal.style.display = 'none';
    }
}

// Get all selected tools (including tools not enabled in MCP management)
function getAllSelectedRoleTools() {
    // Save current page state.
    saveCurrentRolePageToolStates();
    
    // textStatus mapGet all selected tools (regardless of whether enabled in MCP management)
    const selectedTools = [];
    roleToolStateMap.forEach((state, toolKey) => {
        if (state.enabled) {
            selectedTools.push({
                key: toolKey,
                name: state.name || toolKey.split('::').pop() || toolKey,
                mcpEnabled: state.mcpEnabled !== false // mcpEnabled false means disabled,otherwise treat as enabled
            });
        }
    });
    
    return selectedTools;
}

// Check and get tools not enabled in MCP management
function getDisabledTools(selectedTools) {
    return selectedTools.filter(tool => {
        const state = roleToolStateMap.get(tool.key);
        // if mcpEnabled is explicitly false,treat as disabled
        return state && state.mcpEnabled === false;
    });
}

// Load all tools into status map (for switching from all tools to partial tools)
async function loadAllToolsToStateMap() {
    try {
        const pageSize = 100; // uselarger page size to reduce request count
        let page = 1;
        let hasMore = true;
        
        // Iterate all pages to get all tools
        while (hasMore) {
            const url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
            const response = await apiFetch(url);
            if (!response.ok) {
                throw new Error('Failed to get tool list');
            }
            
            const result = await response.json();
            
            // Add all tools to status map
            result.tools.forEach(tool => {
                const toolKey = getToolKey(tool);
                if (!roleToolStateMap.has(toolKey)) {
                    // tool is not in map,initialize based on current mode
                    let enabled = false;
                    if (roleUsesAllTools) {
                        // ifuse all tools,and tool is enabled in MCP management,then mark selected
                        enabled = tool.enabled ? true : false;
                    } else {
                        // ifdo not use all tools,mark selected only if tool is in the role configured tool list
                        enabled = roleConfiguredTools.has(toolKey);
                    }
                    roleToolStateMap.set(toolKey, {
                        enabled: enabled,
                        is_external: tool.is_external || false,
                        external_mcp: tool.external_mcp || '',
                        name: tool.name,
                        mcpEnabled: tool.enabled // Save original enabled state from MCP management
                    });
                } else {
                    // tool already in map,update other attributes but keep enabled state
                    const state = roleToolStateMap.get(toolKey);
                    state.is_external = tool.is_external || false;
                    state.external_mcp = tool.external_mcp || '';
                    state.mcpEnabled = tool.enabled; // Update original enabled state from MCP management
                    if (!state.name || state.name === toolKey.split('::').pop()) {
                        state.name = tool.name; // Update tool name
                    }
                }
            });
            
            // checkwhether there are more pages
            if (page >= result.total_pages) {
                hasMore = false;
            } else {
                page++;
            }
        }
    } catch (error) {
        console.error('Failed to load all tools into status map:', error);
        throw error;
    }
}

// Save role
async function saveRole() {
    const name = document.getElementById('role-name').value.trim();
    if (!name) {
        showNotification(_t('roleModal.roleNameRequired'), 'error');
        return;
    }

    const description = document.getElementById('role-description').value.trim();
    let icon = document.getElementById('role-icon').value.trim();
    // Convert emoji to Unicode escape format to match YAML format (such as \U0001F3C6)
    if (icon) {
        // Get code point of first character (handle emoji that may contain multiple characters)
        const codePoint = icon.codePointAt(0);
        if (codePoint && codePoint > 0x7F) {
            // Convert to 8-digit hexadecimal format (\U0001F3C6)
            icon = '\\U' + codePoint.toString(16).toUpperCase().padStart(8, '0');
        }
    }
    const userPrompt = document.getElementById('role-user-prompt').value.trim();
    const enabled = document.getElementById('role-enabled').checked;

    const isEdit = document.getElementById('role-name').disabled;
    
    // checkwhether this is the default role
    const isDefaultRole = name === DEFAULT_ROLE_NAME;
    
    // check whetheris first added role (excluding default role,no user-created roles)
    const isFirstUserRole = !isEdit && !isDefaultRole && roles.filter(r => r.name !== DEFAULT_ROLE_NAME).length === 0;
    
    // default roledoes not save tools field (use all tools)
    // non-default role:ifuse all tools (roleUsesAllToolsastrue),alsodoes not save tools field
    let tools = [];
    let disabledTools = []; // Store tools not enabled in MCP management
    
    if (!isDefaultRole) {
        // Save currentpagestatus
        saveCurrentRolePageToolStates();
        
        // Collect all selected tools (includingnot enabled in MCP management of )
        let allSelectedTools = getAllSelectedRoleTools();
        
        // iffirst added role and no tools selected,default to all tools
        if (isFirstUserRole && allSelectedTools.length === 0) {
            roleUsesAllTools = true;
            showNotification(_t('roleModal.firstRoleNoToolsHint'), 'info');
        } else if (roleUsesAllTools) {
            // ifcurrently using all tools,need to check whether user cancelled some tools
            // Check whether status map has unchecked enabled tools
            let hasUnselectedTools = false;
            roleToolStateMap.forEach((state) => {
                // iftoolsenabled in MCP managementbut not selected,means user cancelled that tool
                if (state.mcpEnabled !== false && !state.enabled) {
                    hasUnselectedTools = true;
                }
            });
            
            // ifusercancelled some enabled tools,switch to partial tools mode
            if (hasUnselectedTools) {
                // Before switching,needLoad all tools into status map
                // so we can correctly save all tool states (except those cancelled by user)
                await loadAllToolsToStateMap();
                
                // Mark all enabled tools selected (exceptuseralreadycancelled of text)
                // Tools cancelled by user have enabled=false in the status map,keep unchanged
                roleToolStateMap.forEach((state, toolKey) => {
                    // iftoolsenabled in MCP management,andStatus mapinnot explicitly marked unselected (that is, enabled is not false)
                    // then mark selected
                    if (state.mcpEnabled !== false && state.enabled !== false) {
                        state.enabled = true;
                    }
                });
                
                roleUsesAllTools = false;
            } else {
                // Even when using all tools,also need to load all tools into status map,to check whether disabled tools are selected
                // this detects whether user manually selected disabled tools
                await loadAllToolsToStateMap();
                
                // check whether there aredisabled toolsismanually selected (enabled is truebutmcpEnabledasfalse)
                let hasDisabledToolsSelected = false;
                roleToolStateMap.forEach((state) => {
                    if (state.enabled && state.mcpEnabled === false) {
                        hasDisabledToolsSelected = true;
                    }
                });
                
                // ifno disabled tools are selected,Mark all enabled tools selected (this is default behavior for using all tools)
                if (!hasDisabledToolsSelected) {
                    roleToolStateMap.forEach((state) => {
                        if (state.mcpEnabled !== false) {
                            state.enabled = true;
                        }
                    });
                }
                
                // Update allSelectedTools because the status map now contains all tools.
                allSelectedTools = getAllSelectedRoleTools();
            }
        }
        
        // Check which tools are not enabled in MCP management (check regardless of whether using all tools)
        disabledTools = getDisabledTools(allSelectedTools);
        
        // If disabled tools exist, prompt the user.
        if (disabledTools.length > 0) {
            const toolNames = disabledTools.map(t => t.name).join(',');
            const message = `The following ${disabledTools.length} tools are not enabled in MCP management and cannot be configured in roles:\n\n${toolNames}\n\nPlease enable these tools in MCP Management first, then configure them in the role.\n\nContinue saving? (Only enabled tools will be saved)`;
            
            if (!confirm(message)) {
                return; // User cancelled save
            }
        }
        
        // If using all tools, no tool list needs to be fetched.
        if (!roleUsesAllTools) {
            // Get selected tool list (only includes tools enabled in MCP management)
            tools = await getSelectedRoleTools();
        }
    }

    const roleData = {
        name: name,
        description: description,
        icon: icon || undefined, // if empty string, do not send this field
        user_prompt: userPrompt,
        tools: tools, // default role uses an empty array to mean all tools
        enabled: enabled
    };
    const url = isEdit ? `/api/roles/${encodeURIComponent(name)}` : '/api/roles';
    const method = isEdit ? 'PUT' : 'POST';

    try {
        const response = await apiFetch(url, {
            method: method,
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(roleData)
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to save role');
        }

        // If disabled tools were filtered out, notify the user.
        if (disabledTools.length > 0) {
            let toolNames = disabledTools.map(t => t.name).join(',');
            // If the tool name list is too long, truncate it for display.
            if (toolNames.length > 100) {
                toolNames = toolNames.substring(0, 100) + '...';
            }
            showNotification(
                `${isEdit ? 'Role updated' : 'Role created'}, but filtered out ${disabledTools.length} tools that are not enabled in MCP management: ${toolNames}. Please enable these tools in MCP Management first, then configure them in the role.`,
                'warning'
            );
        } else {
            showNotification(isEdit ? 'Role updated' : 'Role created', 'success');
        }
        
        closeRoleModal();
        await refreshRoles();
    } catch (error) {
        console.error('Failed to save role:', error);
        showNotification('Failed to save role: ' + error.message, 'error');
    }
}

// Deleterole
async function deleteRole(roleName) {
    if (roleName === DEFAULT_ROLE_NAME) {
        showNotification(_t('roleModal.cannotDeleteDefaultRole'), 'error');
        return;
    }

    if (!confirm(`Are you sure you want to delete role"${roleName}"? This action cannot be undone.`)) {
        return;
    }

    try {
        const response = await apiFetch(`/api/roles/${encodeURIComponent(roleName)}`, {
            method: 'DELETE'
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to delete role');
        }

        showNotification('Role deleted', 'success');
        
        // If the deleted role is currently selected,switch to default role
        if (currentRole === roleName) {
            handleRoleChange('');
        }

        await refreshRoles();
    } catch (error) {
        console.error('Failed to delete role:', error);
        showNotification('Failed to delete role: ' + error.message, 'error');
    }
}

// Initialize role list when switching pages
if (typeof window.switchPage === 'function') {
    const originalSwitchPage = window.switchPage;
    window.switchPage = function(page) {
        originalSwitchPage(page);
        if (page === 'roles-management') {
            loadRoles().then(() => renderRolesList());
        }
    };
}

// Click outside the modal to close
document.addEventListener('click', (e) => {
    const roleSelectModal = document.getElementById('role-select-modal');
    if (roleSelectModal && e.target === roleSelectModal) {
        closeRoleSelectModal();
    }

    const roleModal = document.getElementById('role-modal');
    if (roleModal && e.target === roleModal) {
        closeRoleModal();
    }

    // Click outside role selector panel to close (must use #role-selector-wrapper,do not use .role-selector-wrapper:the project selector also has that class)
    if (isRoleSelectionPanelOpen()) {
        const roleSelectorWrapper = getChatRoleSelectorWrapper();
        if (!roleSelectorWrapper?.contains(e.target)) {
            closeRoleSelectionPanel();
        }
    }
});

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
    loadRoles();
    updateRoleSelectorDisplay();
});

// Refresh role selector and"Select role"list text after language changes
document.addEventListener('languagechange', () => {
    updateRoleSelectorDisplay();
    renderRoleSelectionSidebar();
});

// Get current selected role (for chat.js)
function getCurrentRole() {
    return currentRole || '';
}

// Expose functions to global scope
if (typeof window !== 'undefined') {
    window.getCurrentRole = getCurrentRole;
    window.toggleRoleSelectionPanel = toggleRoleSelectionPanel;
    window.closeRoleSelectionPanel = closeRoleSelectionPanel;
    window.filterRoleToolsByStatus = filterRoleToolsByStatus;
    window.currentSelectedRole = getCurrentRole();
    
    // Listen for role changes,update global variables
    const originalHandleRoleChange = handleRoleChange;
    handleRoleChange = function(roleName) {
        originalHandleRoleChange(roleName);
        if (typeof window !== 'undefined') {
            window.currentSelectedRole = getCurrentRole();
        }
    };
}
