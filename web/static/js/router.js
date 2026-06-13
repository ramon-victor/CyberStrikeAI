// Page routing management
let currentPage = 'dashboard';

/** Chat and vulnerability management pages preserve the query string on the current hash when switching (such as ?conversation= / ?conversation_id=) */
function buildHashForPage(pageId) {
    if (pageId !== 'chat' && pageId !== 'vulnerabilities') {
        return pageId;
    }
    const full = window.location.hash.slice(1);
    const parts = full.split('?');
    const curPage = parts[0];
    const q = parts.length > 1 ? parts.slice(1).join('?') : '';
    if (curPage === pageId && q) {
        return pageId + '?' + q;
    }
    return pageId;
}

let chatConversationFromHashSeq = 0;
function scheduleChatConversationFromHash(delayMs) {
    const hash = window.location.hash.slice(1);
    const hashParts = hash.split('?');
    if (hashParts[0] !== 'chat' || hashParts.length < 2) {
        return;
    }
    const params = new URLSearchParams(hashParts.slice(1).join('?'));
    const conversationId = params.get('conversation');
    const projectId = params.get('project');
    if (projectId && typeof setActiveProjectId === 'function') {
        setActiveProjectId(projectId);
        if (typeof refreshChatProjectSelector === 'function') {
            refreshChatProjectSelector();
        }
    }
    if (!conversationId) {
        return;
    }
    const token = ++chatConversationFromHashSeq;
    setTimeout(() => {
        if (token !== chatConversationFromHashSeq) {
            return;
        }
        if (typeof loadConversation === 'function') {
            loadConversation(conversationId);
        } else if (typeof window.loadConversation === 'function') {
            window.loadConversation(conversationId);
        } else {
            console.warn('loadConversation function not found');
        }
    }, delayMs);
}

// Initialize router
function initRouter() {
    // Read page from URL hash (if present)
    const hash = window.location.hash.slice(1);
    if (hash) {
        const hashParts = hash.split('?');
        let pageId = hashParts[0];
        if (pageId === 'c2') pageId = 'c2-listeners';
        if (pageId && ['dashboard', 'chat', 'hitl', 'info-collect', 'projects', 'vulnerabilities', 'webshell', 'chat-files', 'mcp-monitor', 'mcp-management', 'knowledge-management', 'knowledge-retrieval-logs', 'roles-management', 'skills-monitor', 'skills-management', 'agents-management', 'settings', 'tasks', 'c2-listeners', 'c2-sessions', 'c2-tasks', 'c2-payloads', 'c2-events', 'c2-profiles'].includes(pageId)) {
            switchPage(pageId);
            if (pageId === 'chat') {
                scheduleChatConversationFromHash(500);
            }
            return;
        }
    }
    
    // Show dashboard by default
    switchPage('dashboard');
}

// Switch page
function switchPage(pageId) {
    if (typeof window.syncC2NavOnceFromServer === 'function') {
        void window.syncC2NavOnceFromServer();
    }
    // Hide all pages
    document.querySelectorAll('.page').forEach(page => {
        page.classList.remove('active');
    });
    
    // Show target page
    const targetPage = document.getElementById(`page-${pageId}`);
    if (targetPage) {
        targetPage.classList.add('active');
        currentPage = pageId;
        
        const newHash = buildHashForPage(pageId);
        if (window.location.hash.slice(1) !== newHash) {
            window.location.hash = newHash;
        }
        
        // Update navigation state
        updateNavState(pageId);
        
        // Page-specific initialization
        initPage(pageId);
    }
}
window.switchPage = switchPage;

// Update navigation state
function updateNavState(pageId) {
    // Remove all active states
    document.querySelectorAll('.nav-item').forEach(item => {
        item.classList.remove('active');
        item.classList.remove('expanded');
    });
    
    document.querySelectorAll('.nav-submenu-item').forEach(item => {
        item.classList.remove('active');
    });
    
    // Set active state
    if (pageId === 'mcp-monitor' || pageId === 'mcp-management') {
        // MCPsubmenu item
        const mcpItem = document.querySelector('.nav-item[data-page="mcp"]');
        if (mcpItem) {
            mcpItem.classList.add('active');
            // Expand MCP submenu
            mcpItem.classList.add('expanded');
        }
        
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else if (pageId === 'knowledge-management' || pageId === 'knowledge-retrieval-logs') {
        // knowledgesubmenu item
        const knowledgeItem = document.querySelector('.nav-item[data-page="knowledge"]');
        if (knowledgeItem) {
            knowledgeItem.classList.add('active');
            // Expand knowledge submenu
            knowledgeItem.classList.add('expanded');
        }
        
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else if (pageId === 'skills-monitor' || pageId === 'skills-management') {
        // Skillssubmenu item
        const skillsItem = document.querySelector('.nav-item[data-page="skills"]');
        if (skillsItem) {
            skillsItem.classList.add('active');
            // Expand Skills submenu
            skillsItem.classList.add('expanded');
        }
        
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else if (pageId === 'agents-management') {
        const agentsItem = document.querySelector('.nav-item[data-page="agents"]');
        if (agentsItem) {
            agentsItem.classList.add('active');
            agentsItem.classList.add('expanded');
        }
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else if (pageId.startsWith('c2') || pageId === 'c2-listeners' || pageId === 'c2-sessions' || pageId === 'c2-tasks' || pageId === 'c2-payloads' || pageId === 'c2-events' || pageId === 'c2-profiles') {
        // C2 submenu item
        const c2Item = document.querySelector('.nav-item[data-page="c2"]');
        if (c2Item) {
            c2Item.classList.add('active');
            c2Item.classList.add('expanded');
        }
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else if (pageId === 'roles-management') {
        // rolesubmenu item
        const rolesItem = document.querySelector('.nav-item[data-page="roles"]');
        if (rolesItem) {
            rolesItem.classList.add('active');
            // Expand role submenu
            rolesItem.classList.add('expanded');
        }
        
        const submenuItem = document.querySelector(`.nav-submenu-item[data-page="${pageId}"]`);
        if (submenuItem) {
            submenuItem.classList.add('active');
        }
    } else {
        // main menu item
        const navItem = document.querySelector(`.nav-item[data-page="${pageId}"]`);
        if (navItem) {
            navItem.classList.add('active');
        }
    }
}

/** Read sidebar submenu items (only .nav-submenu inside,avoid false matches) */
function getNavSubmenuItems(navItem) {
    if (!navItem) return [];
    const submenu = navItem.querySelector('.nav-submenu');
    if (!submenu) return [];
    return Array.from(submenu.querySelectorAll('.nav-submenu-item'));
}

/** Go directly when there is only one child page, avoid the expanded menu being hidden at the bottom of the sidebar */
function navigateSingleSubmenuPage(navItem) {
    const items = getNavSubmenuItems(navItem);
    if (items.length !== 1) return false;
    const pageId = items[0].getAttribute('data-page');
    if (!pageId) return false;
    switchPage(pageId);
    return true;
}

// Toggle submenu
function toggleSubmenu(menuId) {
    const sidebar = document.getElementById('main-sidebar');
    const navItem = document.querySelector(`.nav-item[data-page="${menuId}"]`);
    
    if (!navItem) return;
    
    const collapsed = sidebar && sidebar.classList.contains('collapsed');

    // Check whether the sidebar is collapsed
    if (collapsed) {
        // Show a popup menu while collapsed
        showSubmenuPopup(navItem, menuId);
        return;
    }

    // Expanded sidebar with only one child item (roles, Agents, etc.): single click enters directly, no need to click a second-level menu
    if (navigateSingleSubmenuPage(navItem)) {
        return;
    }

    // Toggle submenu while expanded, and scroll into view so child items are visible
    const willExpand = !navItem.classList.contains('expanded');
    navItem.classList.toggle('expanded');
    if (willExpand) {
        requestAnimationFrame(() => {
            navItem.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
            const items = getNavSubmenuItems(navItem);
            const last = items[items.length - 1];
            if (last) {
                last.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
            }
        });
    }
}
window.toggleSubmenu = toggleSubmenu;

// Show submenu popup
function showSubmenuPopup(navItem, menuId) {
    const existingPopup = document.querySelector('.submenu-popup');
    if (existingPopup) {
        const sameMenu = existingPopup.dataset.menuId === menuId;
        existingPopup.remove();
        // Click the same item again:close only;click another item:continue opening the new menu
        if (sameMenu) {
            return;
        }
    }

    const navItemContent = navItem.querySelector('.nav-item-content');
    const submenu = navItem.querySelector('.nav-submenu');
    
    if (!submenu) return;
    
    // Get menu position
    const rect = navItemContent.getBoundingClientRect();
    
    // Create popup menu
    const popup = document.createElement('div');
    popup.className = 'submenu-popup';
    popup.dataset.menuId = menuId;
    popup.style.position = 'fixed';
    popup.style.left = (rect.right + 8) + 'px';
    popup.style.top = rect.top + 'px';
    popup.style.zIndex = '1000';
    
    // Copy submenu items into popup menu
    const submenuItems = submenu.querySelectorAll('.nav-submenu-item');
    submenuItems.forEach(item => {
        const popupItem = document.createElement('div');
        popupItem.className = 'submenu-popup-item';
        popupItem.textContent = item.textContent.trim();
        
        // Check whether this is the active page
        const pageId = item.getAttribute('data-page');
        if (pageId && document.querySelector(`.nav-submenu-item[data-page="${pageId}"].active`)) {
            popupItem.classList.add('active');
        }
        
        popupItem.onclick = function(e) {
            e.stopPropagation();
            e.preventDefault();
            
            // Get page ID and switch
            const pageId = item.getAttribute('data-page');
            if (pageId) {
                switchPage(pageId);
            }
            
            // Close popup menu
            popup.remove();
            document.removeEventListener('click', closePopup);
        };
        popup.appendChild(popupItem);
    });
    
    document.body.appendChild(popup);
    
    // Click outside to close popup menu
    const closePopup = function(e) {
        if (!popup.contains(e.target) && !navItem.contains(e.target)) {
            popup.remove();
            document.removeEventListener('click', closePopup);
        }
    };
    
    // Delay adding event listener,avoid immediate trigger
    setTimeout(() => {
        document.addEventListener('click', closePopup);
    }, 0);
}

// Initialize page
async function initPage(pageId) {
    // Wait for i18n readiness,avoidfast refresh may display raw placeholder keys before translation functions initialize
    if (window.i18nReady) await window.i18nReady;
    if (typeof stopExternalMcpPoll === 'function') {
        stopExternalMcpPoll();
    }
    switch(pageId) {
        case 'dashboard':
            if (typeof refreshDashboard === 'function') {
                refreshDashboard();
            }
            break;
        case 'chat':
            // Restore chat list collapsed state (preserve user choice when returning from other pages)
            initConversationSidebarState();
            if (typeof prefetchProjectsForChat === 'function') {
                prefetchProjectsForChat();
            }
            if (typeof refreshChatProjectSelector === 'function') {
                refreshChatProjectSelector();
            }
            break;
        case 'hitl':
            if (typeof refreshHitlPending === 'function') {
                refreshHitlPending();
            }
            break;
        case 'info-collect':
            // Information collection page
            if (typeof initInfoCollectPage === 'function') {
                initInfoCollectPage();
            }
            break;
        case 'tasks':
            // Initialize task management page
            if (typeof initTasksPage === 'function') {
                initTasksPage();
            }
            break;
        case 'mcp-monitor':
            // Initialize monitoring panel
            if (typeof refreshMonitorPanel === 'function') {
                refreshMonitorPanel();
            }
            break;
        case 'mcp-management':
            // Initialize MCP management
            const startLoadMcpTools = () => {
                // Load tool list (MCP tool config has moved to MCP management page)
                // Use async loading,avoid blocking page rendering
                if (typeof loadToolsList === 'function') {
                    // Ensure tool pagination settings are initialized
                    if (typeof getToolsPageSize === 'function' && typeof toolsPagination !== 'undefined') {
                        toolsPagination.pageSize = getToolsPageSize();
                    }
                    // Delayed load,let the page render first
                    setTimeout(() => {
                        loadToolsList(1, '').catch(err => {
                            console.error('Failed to load tool list:', err);
                        });
                    }, 100);
                }
            };
            const afterMcpConfigReady = () => {
                startLoadMcpTools();
                if (typeof loadExternalMCPs === 'function') {
                    loadExternalMCPs().catch(err => {
                        console.warn('Failed to load external MCP list:', err);
                    });
                }
                if (typeof startExternalMcpPoll === 'function') {
                    startExternalMcpPoll();
                }
            };
            // Fetch global config first, ensure persistent tool_search state is shown according to the backend effective set
            if (typeof loadConfig === 'function') {
                loadConfig(false)
                    .catch(err => {
                        console.warn('Failed to load config (will continue loading tool list):', err);
                    })
                    .finally(afterMcpConfigReady);
            } else {
                afterMcpConfigReady();
            }
            break;
        case 'projects':
            if (typeof initProjectsPage === 'function') {
                initProjectsPage();
            }
            break;
        case 'vulnerabilities':
            // Initialize vulnerability management page
            if (typeof initVulnerabilityPage === 'function') {
                initVulnerabilityPage();
            }
            break;
        case 'webshell':
            // Initialize WebShell management page
            if (typeof initWebshellPage === 'function') {
                initWebshellPage();
            }
            break;
        case 'chat-files':
            if (typeof initChatFilesPage === 'function') {
                initChatFilesPage();
            }
            break;
        case 'settings':
            // Initialize settings page (does not need loading tool list)
            if (typeof loadConfig === 'function') {
                loadConfig(false);
            }
            break;
        case 'roles-management':
            // Initialize role management page
            // Reset search UI (variables update automatically on the next search)
            const rolesSearchInput = document.getElementById('roles-search');
            if (rolesSearchInput) {
                rolesSearchInput.value = '';
            }
            const rolesSearchClear = document.getElementById('roles-search-clear');
            if (rolesSearchClear) {
                rolesSearchClear.style.display = 'none';
            }
            if (typeof loadRoles === 'function') {
                loadRoles().then(() => {
                    if (typeof renderRolesList === 'function') {
                        renderRolesList();
                    }
                });
            }
            break;
        case 'skills-monitor':
            // Initialize Skills status monitoring page
            if (typeof loadSkillsMonitor === 'function') {
                loadSkillsMonitor();
            }
            break;
        case 'skills-management':
            // Initialize Skills management page
            // Reset search UI (variables update automatically on the next search)
            const skillsSearchInput = document.getElementById('skills-search');
            if (skillsSearchInput) {
                skillsSearchInput.value = '';
            }
            const skillsSearchClear = document.getElementById('skills-search-clear');
            if (skillsSearchClear) {
                skillsSearchClear.style.display = 'none';
            }
            if (typeof initSkillsPagination === 'function') {
                initSkillsPagination();
            }
            if (typeof loadSkills === 'function') {
                loadSkills();
            }
            break;
        case 'agents-management':
            if (typeof loadMarkdownAgents === 'function') {
                loadMarkdownAgents();
            }
            break;
        case 'c2-listeners':
        case 'c2-sessions':
        case 'c2-tasks':
        case 'c2-payloads':
        case 'c2-events':
        case 'c2-profiles':
            window.currentPageId = pageId;
            if (window.C2 && typeof window.C2.init === 'function') {
                window.C2.init();
            }
            break;
    }
    
    // Clean up timers from other pages
    if (pageId !== 'tasks' && typeof cleanupTasksPage === 'function') {
        cleanupTasksPage();
    }
}

// Initialize router after page load
document.addEventListener('DOMContentLoaded', function() {
    initRouter();
    initSidebarState();
    
    // Listen for hash changes
    window.addEventListener('hashchange', function() {
        const hash = window.location.hash.slice(1);
        // Handle hash with parameters (such as chat?conversation=xxx)
        const hashParts = hash.split('?');
        let pageId = hashParts[0];
        
        if (pageId === 'c2') pageId = 'c2-listeners';
        if (pageId && ['dashboard', 'chat', 'hitl', 'info-collect', 'tasks', 'vulnerabilities', 'webshell', 'chat-files', 'mcp-monitor', 'mcp-management', 'knowledge-management', 'knowledge-retrieval-logs', 'roles-management', 'skills-monitor', 'skills-management', 'agents-management', 'settings', 'c2-listeners', 'c2-sessions', 'c2-tasks', 'c2-payloads', 'c2-events', 'c2-profiles'].includes(pageId)) {
            switchPage(pageId);
            if (pageId === 'chat') {
                scheduleChatConversationFromHash(200);
            }
        }
    });
});

// Toggle sidebar collapse/Expand
function toggleSidebar() {
    const sidebar = document.getElementById('main-sidebar');
    if (sidebar) {
        sidebar.classList.toggle('collapsed');
        // Save collapsed state to localStorage
        const isCollapsed = sidebar.classList.contains('collapsed');
        localStorage.setItem('sidebarCollapsed', isCollapsed ? 'true' : 'false');
    }
}
window.toggleSidebar = toggleSidebar;

// Initialize sidebar state
function initSidebarState() {
    const sidebar = document.getElementById('main-sidebar');
    if (sidebar) {
        const savedState = localStorage.getItem('sidebarCollapsed');
        if (savedState === 'true') {
            sidebar.classList.add('collapsed');
        }
    }
    initConversationSidebarState();
}

// Toggle chat page left list collapse/Expand
function toggleConversationSidebar() {
    const sidebar = document.getElementById('conversation-sidebar');
    if (sidebar) {
        sidebar.classList.toggle('collapsed');
        const isCollapsed = sidebar.classList.contains('collapsed');
        localStorage.setItem('conversationSidebarCollapsed', isCollapsed ? 'true' : 'false');
    }
}
window.toggleConversationSidebar = toggleConversationSidebar;

// Restore chat list collapsed state (applies when entering chat page)
function initConversationSidebarState() {
    const sidebar = document.getElementById('conversation-sidebar');
    if (sidebar) {
        const savedState = localStorage.getItem('conversationSidebarCollapsed');
        if (savedState === 'true') {
            sidebar.classList.add('collapsed');
        } else {
            sidebar.classList.remove('collapsed');
        }
    }
}

// Export functions for other scripts (consistent with early binding above, for external script detection)
window.currentPage = function() { return currentPage; };

