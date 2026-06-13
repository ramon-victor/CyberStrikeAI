// Knowledge base management functions
function _t(key, opts) {
    return typeof window.t === 'function' ? window.t(key, opts) : key;
}

// Return"knowledge base disabled"notice block HTML (use data-i18n so language changes update it automatically)
function getKnowledgeNotEnabledHTML() {
    return `
        <div class="empty-state" style="text-align: center; padding: 40px 20px;">
            <div style="font-size: 48px; margin-bottom: 20px;">📚</div>
            <h3 data-i18n="knowledge.notEnabledTitle" style="margin-bottom: 10px; color: #666;"></h3>
            <p data-i18n="knowledge.notEnabledHint" style="color: #999; margin-bottom: 20px;"></p>
            <button data-i18n="knowledge.goToSettings" onclick="switchToSettings()" style="
                background: #007bff;
                color: white;
                border: none;
                padding: 10px 20px;
                border-radius: 5px;
                cursor: pointer;
                font-size: 14px;
            "></button>
        </div>
    `;
}

// Render"knowledge base disabled"state into the container,and apply the current language
function renderKnowledgeNotEnabledState(container) {
    if (!container) return;
    container.innerHTML = getKnowledgeNotEnabledHTML();
    if (typeof window.applyTranslations === 'function') {
        window.applyTranslations(container);
    }
}

let knowledgeCategories = [];
let knowledgeItems = [];
let currentEditingItemId = null;
let isSavingKnowledgeItem = false; // Prevent duplicate submissions
let retrievalLogsData = []; // Store retrieval log data for detail views
let knowledgePagination = {
    currentPage: 1,
    pageSize: 10, // categories per page (changed to category pagination)
    total: 0,
    currentCategory: ''
};
let knowledgeSearchTimeout = null; // search debounce timer

// Load knowledge categories
async function loadKnowledgeCategories() {
    try {
        // Add a timestamp parameter to avoid caching
        const timestamp = Date.now();
        const response = await apiFetch(`/api/knowledge/categories?_t=${timestamp}`, {
            method: 'GET',
            headers: {
                'Cache-Control': 'no-cache, no-store, must-revalidate',
                'Pragma': 'no-cache',
                'Expires': '0'
            }
        });
        if (!response.ok) {
            throw new Error('Failed to get categories');
        }
        const data = await response.json();
        
        // Check whether the knowledge base feature is enabled
        if (data.enabled === false) {
            // Feature is disabled,show a friendly notice (use data-i18n,language changes update it automatically)
            renderKnowledgeNotEnabledState(document.getElementById('knowledge-items-list'));
            return [];
        }
        
        knowledgeCategories = data.categories || [];
        
        // Update the category filter dropdown
        const filterDropdown = document.getElementById('knowledge-category-filter-dropdown');
        if (filterDropdown) {
            filterDropdown.innerHTML = '<div class="custom-select-option" data-value="" onclick="selectKnowledgeCategory(\'\')">All</div>';
            knowledgeCategories.forEach(category => {
                const option = document.createElement('div');
                option.className = 'custom-select-option';
                option.setAttribute('data-value', category);
                option.textContent = category;
                option.onclick = function() {
                    selectKnowledgeCategory(category);
                };
                filterDropdown.appendChild(option);
            });
        }
        
        return knowledgeCategories;
    } catch (error) {
        console.error('Failed to load categories:', error);
        // Show errors only when the feature is not disabled
        if (!error.message.includes('\u77e5\u8bc6\u5e93\u529f\u80fd\u672a\u542f\u7528')) {
            showNotification('Failed to load categories: ' + error.message, 'error');
        }
        return [];
    }
}

// Load knowledge item list (supports category pagination,does not load full content by default)
async function loadKnowledgeItems(category = '', page = 1, pageSize = 10) {
    try {
        // Update pagination state
        knowledgePagination.currentCategory = category;
        knowledgePagination.currentPage = page;
        knowledgePagination.pageSize = pageSize;
        
        // Build URL (category pagination mode,without full content)
        const timestamp = Date.now();
        const offset = (page - 1) * pageSize;
        let url = `/api/knowledge/items?categoryPage=true&limit=${pageSize}&offset=${offset}&_t=${timestamp}`;
        if (category) {
            url += `&category=${encodeURIComponent(category)}`;
        }
        
        const response = await apiFetch(url, {
            method: 'GET',
            headers: {
                'Cache-Control': 'no-cache, no-store, must-revalidate',
                'Pragma': 'no-cache',
                'Expires': '0'
            }
        });
        
        if (!response.ok) {
            throw new Error('Failed to get knowledge items');
        }
        const data = await response.json();
        
        // Check whether the knowledge base feature is enabled
        if (data.enabled === false) {
            // Feature is disabled,show a friendly notice (if it is not already shown;use data-i18n,language changes update it automatically)
            const container = document.getElementById('knowledge-items-list');
            if (container && !container.querySelector('.empty-state')) {
                renderKnowledgeNotEnabledState(container);
            }
            knowledgeItems = [];
            knowledgePagination.total = 0;
            renderKnowledgePagination();
            return [];
        }
        
        // Process category-paginated response data
        const categoriesWithItems = data.categories || [];
        knowledgePagination.total = data.total || 0; // total category count
        
        renderKnowledgeItemsByCategories(categoriesWithItems);
        
        // If a single category is selected,hide pagination (becauseonly one category is shown)
        if (category) {
            const paginationContainer = document.getElementById('knowledge-pagination');
            if (paginationContainer) {
                paginationContainer.innerHTML = '';
            }
        } else {
            renderKnowledgePagination();
        }
        return categoriesWithItems;
    } catch (error) {
        console.error('Failed to load knowledge items:', error);
        // Show errors only when the feature is not disabled
        if (!error.message.includes('\u77e5\u8bc6\u5e93\u529f\u80fd\u672a\u542f\u7528')) {
            showNotification('Failed to load knowledge items: ' + error.message, 'error');
        }
        return [];
    }
}

// Render knowledge item list (category-paginated data structure)
function renderKnowledgeItemsByCategories(categoriesWithItems) {
    const container = document.getElementById('knowledge-items-list');
    if (!container) return;
    
    if (categoriesWithItems.length === 0) {
        container.innerHTML = '<div class="empty-state">No knowledge items</div>';
        return;
    }
    
    // Calculate total item and category counts
    const totalItems = categoriesWithItems.reduce((sum, cat) => sum + (cat.items?.length || 0), 0);
    const categoryCount = categoriesWithItems.length;
    
    // Update statistics
    updateKnowledgeStats(categoriesWithItems, categoryCount);
    
    // Render categories and knowledge items
    let html = '<div class="knowledge-categories-container">';
    
    categoriesWithItems.forEach(categoryData => {
        const category = categoryData.category || 'Uncategorized';
        const categoryItems = categoryData.items || [];
        const categoryCount = categoryData.itemCount || categoryItems.length;
        
        html += `
            <div class="knowledge-category-section" data-category="${escapeHtml(category)}">
                <div class="knowledge-category-header">
                    <div class="knowledge-category-info">
                        <h3 class="knowledge-category-title">${escapeHtml(category)}</h3>
                        <span class="knowledge-category-count">${categoryCount} items</span>
                    </div>
                </div>
                <div class="knowledge-items-grid">
                    ${categoryItems.map(item => renderKnowledgeItemCard(item)).join('')}
                </div>
            </div>
        `;
    });
    
    html += '</div>';
    container.innerHTML = html;
}

// Render knowledge item list (backward-compatible, for legacy item pagination code)
function renderKnowledgeItems(items) {
    const container = document.getElementById('knowledge-items-list');
    if (!container) return;
    
    if (items.length === 0) {
        container.innerHTML = '<div class="empty-state">No knowledge items</div>';
        return;
    }
    
    // Group by category
    const groupedByCategory = {};
    items.forEach(item => {
        const category = item.category || 'Uncategorized';
        if (!groupedByCategory[category]) {
            groupedByCategory[category] = [];
        }
        groupedByCategory[category].push(item);
    });
    
    // Update statistics
    updateKnowledgeStats(items, Object.keys(groupedByCategory).length);
    
    // Render grouped content
    const categories = Object.keys(groupedByCategory).sort();
    let html = '<div class="knowledge-categories-container">';
    
    categories.forEach(category => {
        const categoryItems = groupedByCategory[category];
        const categoryCount = categoryItems.length;
        
        html += `
            <div class="knowledge-category-section" data-category="${escapeHtml(category)}">
                <div class="knowledge-category-header">
                    <div class="knowledge-category-info">
                        <h3 class="knowledge-category-title">${escapeHtml(category)}</h3>
                        <span class="knowledge-category-count">${categoryCount} items</span>
                    </div>
                </div>
                <div class="knowledge-items-grid">
                    ${categoryItems.map(item => renderKnowledgeItemCard(item)).join('')}
                </div>
            </div>
        `;
    });
    
    html += '</div>';
    container.innerHTML = html;
}

// Render pagination controls (category pagination)
function renderKnowledgePagination() {
    const container = document.getElementById('knowledge-pagination');
    if (!container) return;
    
    const { currentPage, pageSize, total } = knowledgePagination;
    const totalPages = Math.ceil(total / pageSize); // totalis the total category count
    
    if (totalPages <= 1) {
        container.innerHTML = '';
        return;
    }
    
    let html = '<div class="knowledge-pagination" style="display: flex; justify-content: center; align-items: center; gap: 8px; padding: 20px; flex-wrap: wrap;">';
    
    // Previous-page button
    html += `<button class="pagination-btn" onclick="loadKnowledgePage(${currentPage - 1})" ${currentPage <= 1 ? 'disabled style="opacity: 0.5; cursor: not-allowed;"' : ''}>Previous</button>`;
    
    // Page number display (show category count)
    html += `<span style="padding: 0 12px;">Page ${currentPage} of ${totalPages} (${total} categories)</span>`;
    
    // Next-page button
    html += `<button class="pagination-btn" onclick="loadKnowledgePage(${currentPage + 1})" ${currentPage >= totalPages ? 'disabled style="opacity: 0.5; cursor: not-allowed;"' : ''}>Next</button>`;
    
    html += '</div>';
    container.innerHTML = html;
}

// Load knowledge items for the specified page
function loadKnowledgePage(page) {
    const { currentCategory, pageSize, total } = knowledgePagination;
    const totalPages = Math.ceil(total / pageSize);
    
    if (page < 1 || page > totalPages) {
        return;
    }
    
    loadKnowledgeItems(currentCategory, page, pageSize);
}

// Render a single knowledge item card
function renderKnowledgeItemCard(item) {
    // Extract content preview (if item has no content field,it is a summary,do not show a preview)
    let previewText = '';
    if (item.content) {
        // Remove markdown formatting,take the first 150 characters
        let preview = item.content;
        // Remove markdown heading markers
        preview = preview.replace(/^#+\s+/gm, '');
        // Remove code blocks
        preview = preview.replace(/```[\s\S]*?```/g, '');
        // Remove inline code
        preview = preview.replace(/`[^`]+`/g, '');
        // Remove links
        preview = preview.replace(/\[([^\]]+)\]\([^\)]+\)/g, '$1');
        // Clean extra whitespace
        preview = preview.replace(/\n+/g, ' ').replace(/\s+/g, ' ').trim();
        
        previewText = preview.length > 150 ? preview.substring(0, 150) + '...' : preview;
    }
    
    // Extract file path display
    const filePath = item.filePath || '';
    const relativePath = filePath.split(/[/\\]/).slice(-2).join('/'); // show the last two path segments
    
    // Format time
    const createdTime = formatTime(item.createdAt);
    const updatedTime = formatTime(item.updatedAt);
    
    // Prefer showing the updated time,show created time when updated time is unavailable
    const displayTime = updatedTime || createdTime;
    const timeLabel = updatedTime ? 'Updated' : 'Created';
    
    // Determine whether this is recently updated (within 7 days)
    let isRecent = false;
    if (item.updatedAt && updatedTime) {
        const updateDate = new Date(item.updatedAt);
        if (!isNaN(updateDate.getTime())) {
            isRecent = (Date.now() - updateDate.getTime()) < 7 * 24 * 60 * 60 * 1000;
        }
    }
    
    return `
        <div class="knowledge-item-card" data-id="${item.id}" data-category="${escapeHtml(item.category)}">
            <div class="knowledge-item-card-header">
                <div class="knowledge-item-card-title-row">
                    <h4 class="knowledge-item-card-title" title="${escapeHtml(item.title)}">${escapeHtml(item.title)}</h4>
                    <div class="knowledge-item-card-actions">
                        <button class="knowledge-item-action-btn" onclick="editKnowledgeItem('${item.id}')" title="Edit">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                                <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                            </svg>
                        </button>
                        <button class="knowledge-item-action-btn knowledge-item-delete-btn" onclick="deleteKnowledgeItem('${item.id}')" title="Delete">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                            </svg>
                        </button>
                    </div>
                </div>
                ${relativePath ? `<div class="knowledge-item-path">📁 ${escapeHtml(relativePath)}</div>` : ''}
            </div>
            ${previewText ? `
            <div class="knowledge-item-card-content">
                <p class="knowledge-item-preview">${escapeHtml(previewText)}</p>
            </div>
            ` : ''}
            <div class="knowledge-item-card-footer">
                <div class="knowledge-item-meta">
                    ${displayTime ? `<span class="knowledge-item-time" title="${timeLabel}">🕒 ${displayTime}</span>` : ''}
                    ${isRecent ? '<span class="knowledge-item-badge-new">New</span>' : ''}
                </div>
            </div>
        </div>
    `;
}

// Update statistics (supportscategory-paginated data structure)
function updateKnowledgeStats(data, categoryCount) {
    const statsContainer = document.getElementById('knowledge-stats');
    if (!statsContainer) return;
    
    // Calculate knowledge item count on the current page
    let currentPageItemCount = 0;
    if (Array.isArray(data) && data.length > 0) {
        // Determine whether this is a categoriesWithItems or items array
        if (data[0].category !== undefined && data[0].items !== undefined) {
            // This is category-paginated data
            currentPageItemCount = data.reduce((sum, cat) => sum + (cat.items?.length || 0), 0);
        } else {
            // This is item-paginated data (backward compatible)
            currentPageItemCount = data.length;
        }
    }
    
    // total category count (from pagination information,use the current page category count as fallback only when undefined)
    const totalCategories = (knowledgePagination.total != null) ? knowledgePagination.total : categoryCount;
    
    statsContainer.innerHTML = `
        <div class="knowledge-stat-item">
            <span class="knowledge-stat-label">Total categories</span>
            <span class="knowledge-stat-value">${totalCategories}</span>
        </div>
        <div class="knowledge-stat-item">
            <span class="knowledge-stat-label">Page categories</span>
            <span class="knowledge-stat-value">${categoryCount}</span>
        </div>
        <div class="knowledge-stat-item">
            <span class="knowledge-stat-label">Page items</span>
            <span class="knowledge-stat-value">${currentPageItemCount} items</span>
        </div>
    `;
    
    // Update index progress
    updateIndexProgress();
}

// Update index progress
let indexProgressInterval = null;

async function updateIndexProgress() {
    try {
        const response = await apiFetch('/api/knowledge/index-status', {
            method: 'GET',
            headers: {
                'Cache-Control': 'no-cache, no-store, must-revalidate',
                'Pragma': 'no-cache',
                'Expires': '0'
            }
        });
        
        if (!response.ok) {
            return; // Fail silently,do not affect the main UI
        }
        
        const status = await response.json();
        const progressContainer = document.getElementById('knowledge-index-progress');
        if (!progressContainer) return;
        
        // Check whether the knowledge base feature is enabled
        if (status.enabled === false) {
            // Feature is disabled,hide the progress bar
            progressContainer.style.display = 'none';
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
            return;
        }
        
        const totalItems = status.total_items || 0;
        const indexedItems = status.indexed_items || 0;
        const progressPercent = status.progress_percent || 0;
        const isComplete = status.is_complete || false;
        const lastError = status.last_error || '';
        
        // Check whether the index is being rebuilt (prefer rebuild status)
        const isRebuilding = status.is_rebuilding || false;
        
        if (totalItems === 0) {
            // No knowledge items,hide the progress bar
            progressContainer.style.display = 'none';
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
            return;
        }
        
        // Show progress bar
        progressContainer.style.display = 'block';
        
        // If there is an error message,show the error
        if (lastError) {
            progressContainer.innerHTML = `
                <div class="knowledge-index-progress-error" style="
                    background: #fee;
                    border: 1px solid #fcc;
                    border-radius: 8px;
                    padding: 16px;
                    margin-bottom: 16px;
                ">
                    <div style="display: flex; align-items: center; margin-bottom: 8px;">
                        <span style="font-size: 20px; margin-right: 8px;">❌</span>
                        <span style="font-weight: bold; color: #c00;">Index build failed</span>
                    </div>
                    <div style="color: #666; font-size: 14px; margin-bottom: 12px; line-height: 1.5;">
                        ${escapeHtml(lastError)}
                    </div>
                    <div style="color: #999; font-size: 12px; margin-bottom: 12px;">
                        Possible causes: embedding model configuration error, invalid API key, insufficient balance, etc. Please check your configuration and try again.
                    </div>
                    <div style="display: flex; gap: 8px;">
                        <button onclick="rebuildKnowledgeIndex()" style="
                            background: #007bff;
                            color: white;
                            border: none;
                            padding: 6px 12px;
                            border-radius: 4px;
                            cursor: pointer;
                            font-size: 13px;
                        ">Retry</button>
                        <button onclick="stopIndexProgressPolling()" style="
                            background: #6c757d;
                            color: white;
                            border: none;
                            padding: 6px 12px;
                            border-radius: 4px;
                            cursor: pointer;
                            font-size: 13px;
                        ">Close</button>
                    </div>
                </div>
            `;
            // Stop polling
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
            // Show error notification
            showNotification('Index build failed: ' + lastError.substring(0, 100), 'error');
            return;
        }
        

        // Handle rebuild status first
        if (isRebuilding) {
            const rebuildTotal = status.rebuild_total || totalItems;
            const rebuildCurrent = status.rebuild_current || 0;
            const rebuildFailed = status.rebuild_failed || 0;
            const rebuildLastItemID = status.rebuild_last_item_id || '';
            const rebuildLastChunks = status.rebuild_last_chunks || 0;
            const rebuildStartTime = status.rebuild_start_time || '';

            // Calculate progress percentage (using rebuild progress)
            let rebuildProgress = progressPercent;
            if (rebuildTotal > 0) {
                rebuildProgress = (rebuildCurrent / rebuildTotal) * 100;
            }

            progressContainer.innerHTML = `
                <div class="knowledge-index-progress">
                    <div class="progress-header">
                        <span class="progress-icon">🔨</span>
                        <span class="progress-text">Rebuilding index: ${rebuildCurrent}/${rebuildTotal} (${rebuildProgress.toFixed(1)}%) - Failed: ${rebuildFailed}</span>
                    </div>
                    <div class="progress-bar-container">
                        <div class="progress-bar" style="width: ${rebuildProgress}%"></div>
                    </div>
                    <div class="progress-hint">
                        ${rebuildLastItemID ? `Processing: ${escapeHtml(rebuildLastItemID.substring(0, 36))}... (${rebuildLastChunks} chunks)` : 'Processing...'}
                        ${rebuildStartTime ? `<br>Start time: ${new Date(rebuildStartTime).toLocaleString()}` : ''}
                    </div>
                </div>
            `;

            // Continue polling while rebuilding
            if (!indexProgressInterval) {
                indexProgressInterval = setInterval(updateIndexProgress, 2000);
            }
            return;
        }
        
        if (isComplete) {
            progressContainer.innerHTML = `
                <div class="knowledge-index-progress-complete">
                    <span class="progress-icon">✅</span>
                    <span class="progress-text">Index build complete (${indexedItems}/${totalItems})</span>
                </div>
            `;
            // Stop polling after completion
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
        } else {
            progressContainer.innerHTML = `
                <div class="knowledge-index-progress">
                    <div class="progress-header">
                        <span class="progress-icon">🔨</span>
                        <span class="progress-text">Building index: ${indexedItems}/${totalItems} (${progressPercent.toFixed(1)}%)</span>
                    </div>
                    <div class="progress-bar-container">
                        <div class="progress-bar" style="width: ${progressPercent}%"></div>
                    </div>
                    <div class="progress-hint">Semantic search will be available once indexing is complete</div>
                </div>
            `;
            
            // If polling has not started yet,start polling
            if (!indexProgressInterval) {
                indexProgressInterval = setInterval(updateIndexProgress, 3000); // refresh every 3 seconds
            }
        }
    } catch (error) {
        // Show error information
        console.error('Failed to get index status:', error);
        const progressContainer = document.getElementById('knowledge-index-progress');
        if (progressContainer) {
            progressContainer.style.display = 'block';
            progressContainer.innerHTML = `
                <div class="knowledge-index-progress-error" style="
                    background: #fee;
                    border: 1px solid #fcc;
                    border-radius: 8px;
                    padding: 16px;
                    margin-bottom: 16px;
                ">
                    <div style="display: flex; align-items: center; margin-bottom: 8px;">
                        <span style="font-size: 20px; margin-right: 8px;">⚠️</span>
                        <span style="font-weight: bold; color: #c00;">Cannot get index status</span>
                    </div>
                    <div style="color: #666; font-size: 14px;">
                        Cannot connect to server for index status. Please check network connection or refresh the page.
                    </div>
                </div>
            `;
        }
        // Stop polling
        if (indexProgressInterval) {
            clearInterval(indexProgressInterval);
            indexProgressInterval = null;
        }
    }
}

// Stop index progress polling
function stopIndexProgressPolling() {
    if (indexProgressInterval) {
        clearInterval(indexProgressInterval);
        indexProgressInterval = null;
    }
    const progressContainer = document.getElementById('knowledge-index-progress');
    if (progressContainer) {
        progressContainer.style.display = 'none';
    }
}

// Select knowledge category
function selectKnowledgeCategory(category) {
    const trigger = document.getElementById('knowledge-category-filter-trigger');
    const wrapper = document.getElementById('knowledge-category-filter-wrapper');
    const dropdown = document.getElementById('knowledge-category-filter-dropdown');
    
    if (trigger && wrapper && dropdown) {
        const displayText = category || 'All';
        trigger.querySelector('span').textContent = displayText;
        wrapper.classList.remove('open');
        
        // Update selected state
        dropdown.querySelectorAll('.custom-select-option').forEach(opt => {
            opt.classList.remove('selected');
            if (opt.getAttribute('data-value') === category) {
                opt.classList.add('selected');
            }
        });
    }
    // Reset to the first page when changing category (if a category is selected,the API returns all items in that category)
    loadKnowledgeItems(category, 1, knowledgePagination.pageSize);
}

// Filter knowledge items
function filterKnowledgeItems() {
    const wrapper = document.getElementById('knowledge-category-filter-wrapper');
    if (wrapper) {
        const selectedOption = wrapper.querySelector('.custom-select-option.selected');
        const category = selectedOption ? selectedOption.getAttribute('data-value') : '';
        // Reset to the first page
        loadKnowledgeItems(category, 1, knowledgePagination.pageSize);
    }
}

// Handle search input (with debounce)
function handleKnowledgeSearchInput() {
    const searchInput = document.getElementById('knowledge-search');
    const searchTerm = searchInput?.value.trim() || '';
    
    // Clear the previous timer
    if (knowledgeSearchTimeout) {
        clearTimeout(knowledgeSearchTimeout);
    }
    
    // If the search box is empty,restore the list immediately
    if (!searchTerm) {
        const wrapper = document.getElementById('knowledge-category-filter-wrapper');
        let category = '';
        if (wrapper) {
            const selectedOption = wrapper.querySelector('.custom-select-option.selected');
            category = selectedOption ? selectedOption.getAttribute('data-value') : '';
        }
        loadKnowledgeItems(category, 1, knowledgePagination.pageSize);
        return;
    }
    
    // When there is a search term,run the search after a 500 ms delay (debounce)
    knowledgeSearchTimeout = setTimeout(() => {
        searchKnowledgeItems();
    }, 500);
}

// Search knowledge items (backend keyword matching,search across all data)
async function searchKnowledgeItems() {
    const searchInput = document.getElementById('knowledge-search');
    const searchTerm = searchInput?.value.trim() || '';
    
    if (!searchTerm) {
        // Restore the original list (Reset to the first page)
        const wrapper = document.getElementById('knowledge-category-filter-wrapper');
        let category = '';
        if (wrapper) {
            const selectedOption = wrapper.querySelector('.custom-select-option.selected');
            category = selectedOption ? selectedOption.getAttribute('data-value') : '';
        }
        await loadKnowledgeItems(category, 1, knowledgePagination.pageSize);
        return;
    }
    
    try {
        // Get the currently selected category
        const wrapper = document.getElementById('knowledge-category-filter-wrapper');
        let category = '';
        if (wrapper) {
            const selectedOption = wrapper.querySelector('.custom-select-option.selected');
            category = selectedOption ? selectedOption.getAttribute('data-value') : '';
        }
        
        // Call the backend API for a full search
        const timestamp = Date.now();
        let url = `/api/knowledge/items?search=${encodeURIComponent(searchTerm)}&_t=${timestamp}`;
        if (category) {
            url += `&category=${encodeURIComponent(category)}`;
        }
        
        const response = await apiFetch(url, {
            method: 'GET',
            headers: {
                'Cache-Control': 'no-cache, no-store, must-revalidate',
                'Pragma': 'no-cache',
                'Expires': '0'
            }
        });
        
        if (!response.ok) {
            throw new Error('Search failed');
        }
        
        const data = await response.json();
        
        // Check whether the knowledge base feature is enabled
        if (data.enabled === false) {
            renderKnowledgeNotEnabledState(document.getElementById('knowledge-items-list'));
            return;
        }
        
        // Process search results
        const categoriesWithItems = data.categories || [];
        
        // Render search results
        const container = document.getElementById('knowledge-items-list');
        if (!container) return;
        
        if (categoriesWithItems.length === 0) {
            container.innerHTML = `
                <div class="empty-state" style="text-align: center; padding: 40px 20px;">
                    <div style="font-size: 48px; margin-bottom: 20px;">🔍</div>
                    <h3 style="margin-bottom: 10px;">No matching knowledge items found</h3>
                    <p style="color: #999;">Keyword "<strong>${escapeHtml(searchTerm)}</strong>" had no results across all data</p>
                    <p style="color: #999; margin-top: 10px; font-size: 0.9em;">Please try other keywords or use the category filter</p>
                </div>
            `;
        } else {
            // Calculate total item and category counts
            const totalItems = categoriesWithItems.reduce((sum, cat) => sum + (cat.items?.length || 0), 0);
            const categoryCount = categoriesWithItems.length;
            
            // Update statistics
            updateKnowledgeStats(categoriesWithItems, categoryCount);
            
            // Render search results
            renderKnowledgeItemsByCategories(categoriesWithItems);
        }
        
        // Hide pagination while searching (becausesearch results show all matches)
        const paginationContainer = document.getElementById('knowledge-pagination');
        if (paginationContainer) {
            paginationContainer.innerHTML = '';
        }
        
    } catch (error) {
        console.error('Search knowledge itemsfailed:', error);
        showNotification('Search failed: ' + error.message, 'error');
    }
}

// Refresh knowledge base
async function refreshKnowledgeBase() {
    try {
        showNotification('Scanning knowledge base...', 'info');
        const response = await apiFetch('/api/knowledge/scan', {
            method: 'POST'
        });
        if (!response.ok) {
            throw new Error('Failed to scan knowledge base');
        }
        const data = await response.json();
        // Show different notices based on the returned message
        if (data.items_to_index && data.items_to_index > 0) {
            showNotification(`Scan complete. Starting to index ${data.items_to_index} new or updated knowledge items`, 'success');
        } else {
            showNotification(data.message || 'Scan complete. No new or updated items to index.', 'success');
        }
        // Reload knowledge items (Reset to the first page)
        await loadKnowledgeCategories();
        await loadKnowledgeItems(knowledgePagination.currentCategory, 1, knowledgePagination.pageSize);
        
        // Stop existing polling
        if (indexProgressInterval) {
            clearInterval(indexProgressInterval);
            indexProgressInterval = null;
        }
        
        // If there are items that need indexing,wait briefly and update progress immediately
        if (data.items_to_index && data.items_to_index > 0) {
            await new Promise(resolve => setTimeout(resolve, 500));
            updateIndexProgress();
            // start pollingprogress (refresh every 2 seconds)
            if (!indexProgressInterval) {
                indexProgressInterval = setInterval(updateIndexProgress, 2000);
            }
        } else {
            // No items need indexing,also update once to show current status
            updateIndexProgress();
        }
    } catch (error) {
        console.error('Failed to refresh knowledge base:', error);
        showNotification('Failed to refresh knowledge base: ' + error.message, 'error');
    }
}

// Rebuild index
async function rebuildKnowledgeIndex() {
    try {
        if (!confirm('Are you sure you want to rebuild the index? This may take some time.')) {
            return;
        }
        showNotification('Rebuilding index...', 'info');
        
        // Stop existing polling first
        if (indexProgressInterval) {
            clearInterval(indexProgressInterval);
            indexProgressInterval = null;
        }
        
        // Show immediately"rebuilding"status,becauserebuilding clears the old index at start
        const progressContainer = document.getElementById('knowledge-index-progress');
        if (progressContainer) {
            progressContainer.style.display = 'block';
            progressContainer.innerHTML = `
                <div class="knowledge-index-progress">
                    <div class="progress-header">
                        <span class="progress-icon">🔨</span>
                        <span class="progress-text">Rebuilding index: Preparing...</span>
                    </div>
                    <div class="progress-bar-container">
                        <div class="progress-bar" style="width: 0%"></div>
                    </div>
                    <div class="progress-hint">Semantic search will be available once indexing is complete</div>
                </div>
            `;
        }
        
        const response = await apiFetch('/api/knowledge/index', {
            method: 'POST'
        });
        if (!response.ok) {
            throw new Error('Failed to rebuild index');
        }
        showNotification('Index rebuild started. It will continue in the background.', 'success');
        
        // Wait briefly,ensure the backend has started processing and cleared the old index
        await new Promise(resolve => setTimeout(resolve, 500));
        
        // Update progress once immediately
        updateIndexProgress();
        
        // start pollingprogress (refresh every 2 seconds,more frequently than the default 3 seconds)
        if (!indexProgressInterval) {
            indexProgressInterval = setInterval(updateIndexProgress, 2000);
        }
    } catch (error) {
        console.error('Failed to rebuild index:', error);
        showNotification('Failed to rebuild index: ' + error.message, 'error');
    }
}

// Show add knowledge item modal
function showAddKnowledgeItemModal() {
    currentEditingItemId = null;
    document.getElementById('knowledge-item-modal-title').textContent = 'Add Knowledge';
    document.getElementById('knowledge-item-category').value = '';
    document.getElementById('knowledge-item-title').value = '';
    document.getElementById('knowledge-item-content').value = '';
    document.getElementById('knowledge-item-modal').style.display = 'block';
}

// Edit knowledge item
async function editKnowledgeItem(id) {
    try {
        const response = await apiFetch(`/api/knowledge/items/${id}`);
        if (!response.ok) {
            throw new Error('Failed to get knowledge item');
        }
        const item = await response.json();
        
        currentEditingItemId = id;
        document.getElementById('knowledge-item-modal-title').textContent = 'Edit Knowledge';
        document.getElementById('knowledge-item-category').value = item.category;
        document.getElementById('knowledge-item-title').value = item.title;
        document.getElementById('knowledge-item-content').value = item.content;
        document.getElementById('knowledge-item-modal').style.display = 'block';
    } catch (error) {
        console.error('Edit knowledge itemfailed:', error);
        showNotification('Failed to edit knowledge item: ' + error.message, 'error');
    }
}

// Save knowledge item
async function saveKnowledgeItem() {
    // Prevent duplicate submissions
    if (isSavingKnowledgeItem) {
        showNotification('Saving in progress, please do not click again...', 'warning');
        return;
    }
    
    const category = document.getElementById('knowledge-item-category').value.trim();
    const title = document.getElementById('knowledge-item-title').value.trim();
    const content = document.getElementById('knowledge-item-content').value.trim();
    
    if (!category || !title || !content) {
        showNotification('Please fill in all required fields', 'error');
        return;
    }
    
    // Set saving flag
    isSavingKnowledgeItem = true;
    
    // Get save and cancel buttons
    const saveButton = document.querySelector('#knowledge-item-modal .modal-footer .btn-primary');
    const cancelButton = document.querySelector('#knowledge-item-modal .modal-footer .btn-secondary');
    const modal = document.getElementById('knowledge-item-modal');
    
    const originalButtonText = saveButton ? saveButton.textContent : 'Save';
    const originalButtonDisabled = saveButton ? saveButton.disabled : false;
    
    // Disable all input fields and buttons
    const categoryInput = document.getElementById('knowledge-item-category');
    const titleInput = document.getElementById('knowledge-item-title');
    const contentInput = document.getElementById('knowledge-item-content');
    
    if (categoryInput) categoryInput.disabled = true;
    if (titleInput) titleInput.disabled = true;
    if (contentInput) contentInput.disabled = true;
    if (cancelButton) cancelButton.disabled = true;
    
    // Set save button loading state
    if (saveButton) {
        saveButton.disabled = true;
        saveButton.style.opacity = '0.6';
        saveButton.style.cursor = 'not-allowed';
        saveButton.textContent = 'Saving...';
    }
    
    try {
        const url = currentEditingItemId 
            ? `/api/knowledge/items/${currentEditingItemId}`
            : '/api/knowledge/items';
        const method = currentEditingItemId ? 'PUT' : 'POST';
        
        const response = await apiFetch(url, {
            method: method,
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                category,
                title,
                content
            })
        });
        
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({}));
            throw new Error(errorData.error || 'Failed to save knowledge item');
        }
        
        const item = await response.json();
        const action = currentEditingItemId ? 'Updated' : 'Created';
        const newItemCategory = item.category || category; // Save the newly added knowledge item category
        
        // Get current filter state,to preserve it after refresh
        const currentCategory = document.getElementById('knowledge-category-filter-wrapper');
        let selectedCategory = '';
        if (currentCategory) {
            const selectedOption = currentCategory.querySelector('.custom-select-option.selected');
            if (selectedOption) {
                selectedCategory = selectedOption.getAttribute('data-value') || '';
            }
        }
        
        // Close the modal immediately,give clear feedback to the user
        closeKnowledgeItemModal();
        
        // Show loading state and refresh data (wait for completion to ensure data sync)
        const itemsListContainer = document.getElementById('knowledge-items-list');
        const originalContent = itemsListContainer ? itemsListContainer.innerHTML : '';
        
        if (itemsListContainer) {
            itemsListContainer.innerHTML = '<div class="loading-spinner">Refreshing...</div>';
        }
        
        try {
            // Refresh categories first, then refresh knowledge items
            console.log('Starting knowledge base data refresh...');
            await loadKnowledgeCategories();
            console.log('Category refresh complete; starting knowledge item refresh...');
            
            // If the newly added knowledge item is not in the currently filtered category,switch to that category for display
            let categoryToShow = selectedCategory;
            if (!currentEditingItemId && selectedCategory && selectedCategory !== '' && newItemCategory !== selectedCategory) {
                // If the newly added knowledge item is outside the current filter, switch to the new item category.
                categoryToShow = newItemCategory;
                // Update filter display (without triggering a load, because we load manually below)
                const trigger = document.getElementById('knowledge-category-filter-trigger');
                const wrapper = document.getElementById('knowledge-category-filter-wrapper');
                const dropdown = document.getElementById('knowledge-category-filter-dropdown');
                if (trigger && wrapper && dropdown) {
                    trigger.querySelector('span').textContent = newItemCategory || 'All';
                    dropdown.querySelectorAll('.custom-select-option').forEach(opt => {
                        opt.classList.remove('selected');
                        if (opt.getAttribute('data-value') === newItemCategory) {
                            opt.classList.add('selected');
                        }
                    });
                }
                showNotification(`\u2705 ${action} successful! Switched to category "${newItemCategory}" to view the new knowledge item.`, 'success');
            }
            
            // Refresh knowledge item list (Reset to the first page)
            await loadKnowledgeItems(categoryToShow, 1, knowledgePagination.pageSize);
            console.log('Knowledge item refresh complete');
        } catch (err) {
            console.error('Failed to refresh data:', err);
            // If refresh fails,restore original content
            if (itemsListContainer && originalContent) {
                itemsListContainer.innerHTML = originalContent;
            }
            showNotification('\u26a0\ufe0f Knowledge item saved, but list refresh failed. Please manually refresh the page.', 'warning');
        }
        
    } catch (error) {
        console.error('Failed to save knowledge item:', error);
        showNotification('\u274c Failed to save knowledge item: ' + error.message, 'error');
        
        // If the notification system is unavailable,use alert
        if (typeof window.showNotification !== 'function') {
            alert('\u274c Failed to save knowledge item: ' + error.message);
        }
        
        // Restore input field and button states (do not close the modal on error,let the user modify and retry)
        if (categoryInput) categoryInput.disabled = false;
        if (titleInput) titleInput.disabled = false;
        if (contentInput) contentInput.disabled = false;
        if (cancelButton) cancelButton.disabled = false;
        if (saveButton) {
            saveButton.disabled = false;
            saveButton.style.opacity = '';
            saveButton.style.cursor = '';
            saveButton.textContent = originalButtonText;
        }
    } finally {
        // Clear saving flag
        isSavingKnowledgeItem = false;
    }
}

// Delete knowledge item
async function deleteKnowledgeItem(id) {
    if (!confirm('Are you sure you want to delete this knowledge item?')) {
        return;
    }
    
    // Find the knowledge item card and delete button to delete
    const itemCard = document.querySelector(`.knowledge-item-card[data-id="${id}"]`);
    const deleteButton = itemCard ? itemCard.querySelector('.knowledge-item-delete-btn') : null;
    const categorySection = itemCard ? itemCard.closest('.knowledge-category-section') : null;
    let originalDisplay = '';
    let originalOpacity = '';
    let originalButtonOpacity = '';
    
    // Set delete button loading state
    if (deleteButton) {
        originalButtonOpacity = deleteButton.style.opacity;
        deleteButton.style.opacity = '0.5';
        deleteButton.style.cursor = 'not-allowed';
        deleteButton.disabled = true;
        
        // Add loading animation
        const svg = deleteButton.querySelector('svg');
        if (svg) {
            svg.style.animation = 'spin 1s linear infinite';
        }
    }
    
    // Remove the item from the UI immediately (optimistic update)
    if (itemCard) {
        originalDisplay = itemCard.style.display;
        originalOpacity = itemCard.style.opacity;
        itemCard.style.transition = 'opacity 0.3s ease-out, transform 0.3s ease-out';
        itemCard.style.opacity = '0';
        itemCard.style.transform = 'translateX(-20px)';
        
        // Remove after the animation completes
        setTimeout(() => {
            if (itemCard.parentElement) {
                itemCard.remove();
                
                // Check whether the category still has items,hide the category title if none remain
                if (categorySection) {
                    const remainingItems = categorySection.querySelectorAll('.knowledge-item-card');
                    if (remainingItems.length === 0) {
                        categorySection.style.transition = 'opacity 0.3s ease-out';
                        categorySection.style.opacity = '0';
                        setTimeout(() => {
                            if (categorySection.parentElement) {
                                categorySection.remove();
                            }
                        }, 300);
                    } else {
                        // Update category count
                        const categoryCount = categorySection.querySelector('.knowledge-category-count');
                        if (categoryCount) {
                            const newCount = remainingItems.length;
                            categoryCount.textContent = `${newCount} items`;
                        }
                    }
                }
                
                // Do not update statistics here,wait for reload so the correct logic updates them
            }
        }, 300);
    }
    
    try {
        const response = await apiFetch(`/api/knowledge/items/${id}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({}));
            throw new Error(errorData.error || 'Failed to delete knowledge item');
        }
        
        // Show success notification
        showNotification('\u2705 Deleted successfully! Knowledge item removed from system.', 'success');
        
        // Reload data to ensure synchronization (keep the current page)
        await loadKnowledgeCategories();
        await loadKnowledgeItems(knowledgePagination.currentCategory, knowledgePagination.currentPage, knowledgePagination.pageSize);
        
    } catch (error) {
        console.error('Failed to delete knowledge item:', error);
        
        // If deletion fails,restore the item display
        if (itemCard && originalDisplay !== 'none') {
            itemCard.style.display = originalDisplay || '';
            itemCard.style.opacity = originalOpacity || '1';
            itemCard.style.transform = '';
            itemCard.style.transition = '';
            
            // If the category was removed,needs to be restored
            if (categorySection && !categorySection.parentElement) {
                // reload to restore it (keep the current pagination state)
                await loadKnowledgeItems(knowledgePagination.currentCategory, knowledgePagination.currentPage, knowledgePagination.pageSize);
            }
        }
        
        // Restore delete button state
        if (deleteButton) {
            deleteButton.style.opacity = originalButtonOpacity || '';
            deleteButton.style.cursor = '';
            deleteButton.disabled = false;
            const svg = deleteButton.querySelector('svg');
            if (svg) {
                svg.style.animation = '';
            }
        }
        
        showNotification('\u274c Failed to delete knowledge item: ' + error.message, 'error');
    }
}

// Temporarily update statistics (after delete)
function updateKnowledgeStatsAfterDelete() {
    const statsContainer = document.getElementById('knowledge-stats');
    if (!statsContainer) return;
    
    const allItems = document.querySelectorAll('.knowledge-item-card');
    const allCategories = document.querySelectorAll('.knowledge-category-section');
    
    const totalItems = allItems.length;
    const categoryCount = allCategories.length;
    
    // Calculate total content size (simplified here,should actually be fetched from the server)
    const statsItems = statsContainer.querySelectorAll('.knowledge-stat-item');
    if (statsItems.length >= 2) {
        const totalItemsSpan = statsItems[0].querySelector('.knowledge-stat-value');
        const categoryCountSpan = statsItems[1].querySelector('.knowledge-stat-value');
        
        if (totalItemsSpan) {
            totalItemsSpan.textContent = totalItems;
        }
        if (categoryCountSpan) {
            categoryCountSpan.textContent = categoryCount;
        }
    }
}

// Close knowledge item modal
function closeKnowledgeItemModal() {
    const modal = document.getElementById('knowledge-item-modal');
    if (modal) {
        modal.style.display = 'none';
    }
    
    // Reset editing state
    currentEditingItemId = null;
    isSavingKnowledgeItem = false;
    
    // Restore all input fields and button states
    const categoryInput = document.getElementById('knowledge-item-category');
    const titleInput = document.getElementById('knowledge-item-title');
    const contentInput = document.getElementById('knowledge-item-content');
    const saveButton = document.querySelector('#knowledge-item-modal .modal-footer .btn-primary');
    const cancelButton = document.querySelector('#knowledge-item-modal .modal-footer .btn-secondary');
    
    if (categoryInput) {
        categoryInput.disabled = false;
        categoryInput.value = '';
    }
    if (titleInput) {
        titleInput.disabled = false;
        titleInput.value = '';
    }
    if (contentInput) {
        contentInput.disabled = false;
        contentInput.value = '';
    }
    if (saveButton) {
        saveButton.disabled = false;
        saveButton.style.opacity = '';
        saveButton.style.cursor = '';
        saveButton.textContent = 'Save';
    }
    if (cancelButton) {
        cancelButton.disabled = false;
    }
}

// Load retrieval logs
async function loadRetrievalLogs(conversationId = '', messageId = '') {
    try {
        let url = '/api/knowledge/retrieval-logs?limit=100';
        if (conversationId) {
            url += `&conversationId=${encodeURIComponent(conversationId)}`;
        }
        if (messageId) {
            url += `&messageId=${encodeURIComponent(messageId)}`;
        }
        
        const response = await apiFetch(url);
        if (!response.ok) {
            throw new Error('Failed to get retrieval logs');
        }
        const data = await response.json();
        renderRetrievalLogs(data.logs || []);
    } catch (error) {
        console.error('Load retrieval logsfailed:', error);
        // Even if loading fails,show empty state instead of always showing"Loading..."
        renderRetrievalLogs([]);
        // Show error notifications only for non-empty filters (avoid showing errors when there is no data)
        if (conversationId || messageId) {
            showNotification(_t('retrievalLogs.loadError') + ': ' + error.message, 'error');
        }
    }
}

// Render retrieval logs
function renderRetrievalLogs(logs) {
    const container = document.getElementById('retrieval-logs-list');
    if (!container) return;
    
    // Update statistics (update even for an empty array)
    updateRetrievalStats(logs);
    
    if (logs.length === 0) {
        container.innerHTML = '<div class="empty-state">' + _t('retrievalLogs.noRecords') + '</div>';
        retrievalLogsData = [];
        return;
    }
    
    // Save log data for detail views
    retrievalLogsData = logs;
    
    container.innerHTML = logs.map((log, index) => {
        // Process retrievedItems:may be an array,string array,or special marker
        let itemCount = 0;
        let hasResults = false;
        
        if (log.retrievedItems) {
            if (Array.isArray(log.retrievedItems)) {
                // Filter out special markers
                const realItems = log.retrievedItems.filter(id => id !== '_has_results');
                itemCount = realItems.length;
                // If a special marker exists,it means results exist but IDs are unknown,display as"Has results"
                if (log.retrievedItems.includes('_has_results')) {
                    hasResults = true;
                    // If there are real IDs,use the real count;otherwisedisplay as"Has results" (do not show a specific count)
                    if (itemCount === 0) {
                        itemCount = -1; // -1 means results exist but count is unknown
                    }
                } else {
                    hasResults = itemCount > 0;
                }
            } else if (typeof log.retrievedItems === 'string') {
                // If it is a string,try to parse JSON
                try {
                    const parsed = JSON.parse(log.retrievedItems);
                    if (Array.isArray(parsed)) {
                        const realItems = parsed.filter(id => id !== '_has_results');
                        itemCount = realItems.length;
                        if (parsed.includes('_has_results')) {
                            hasResults = true;
                            if (itemCount === 0) {
                                itemCount = -1;
                            }
                        } else {
                            hasResults = itemCount > 0;
                        }
                    }
                } catch (e) {
                    // Parsing failed,ignore
                }
            }
        }
        
        const timeAgo = getTimeAgo(log.createdAt);
        
        return `
            <div class="retrieval-log-card ${hasResults ? 'has-results' : 'no-results'}" data-index="${index}">
                <div class="retrieval-log-card-header">
                    <div class="retrieval-log-icon">
                        ${hasResults ? '🔍' : '⚠️'}
                    </div>
                    <div class="retrieval-log-main-info">
                        <div class="retrieval-log-query">
                            ${escapeHtml(log.query || _t('retrievalLogs.noQuery'))}
                        </div>
                        <div class="retrieval-log-meta">
                            <span class="retrieval-log-time" title="${formatTime(log.createdAt)}">
                                🕒 ${timeAgo}
                            </span>
                            ${log.riskType ? `<span class="retrieval-log-risk-type">📁 ${escapeHtml(log.riskType)}</span>` : ''}
                        </div>
                    </div>
                    <div class="retrieval-log-result-badge ${hasResults ? 'success' : 'empty'}">
                        ${hasResults ? (itemCount > 0 ? itemCount + ' ' + _t('retrievalLogs.itemsUnit') : _t('retrievalLogs.hasResults')) : _t('retrievalLogs.noResults')}
                    </div>
                </div>
                <div class="retrieval-log-card-body">
                    <div class="retrieval-log-details-grid">
                        ${log.conversationId ? `
                            <div class="retrieval-log-detail-item">
                                <span class="detail-label">${_t('retrievalLogs.conversationId')}</span>
                                <code class="detail-value" title="${_t('retrievalLogs.clickToCopy')}" data-copy-title-copied="${_t('common.copied')}" data-copy-title-click="${_t('retrievalLogs.clickToCopy')}" onclick="var t=this; navigator.clipboard.writeText('${escapeHtml(log.conversationId)}').then(function(){ t.title=t.getAttribute('data-copy-title-copied')||'Copied!'; setTimeout(function(){ t.title=t.getAttribute('data-copy-title-click')||'Click to copy'; }, 2000); });" style="cursor: pointer;">${escapeHtml(log.conversationId)}</code>
                            </div>
                        ` : ''}
                        ${log.messageId ? `
                            <div class="retrieval-log-detail-item">
                                <span class="detail-label">${_t('retrievalLogs.messageId')}</span>
                                <code class="detail-value" title="${_t('retrievalLogs.clickToCopy')}" data-copy-title-copied="${_t('common.copied')}" data-copy-title-click="${_t('retrievalLogs.clickToCopy')}" onclick="var el=this; navigator.clipboard.writeText('${escapeHtml(log.messageId)}').then(function(){ el.title=el.getAttribute('data-copy-title-copied')||el.title; setTimeout(function(){ el.title=el.getAttribute('data-copy-title-click')||el.title; }, 2000); });" style="cursor: pointer;">${escapeHtml(log.messageId)}</code>
                            </div>
                        ` : ''}
                        <div class="retrieval-log-detail-item">
                            <span class="detail-label">${_t('retrievalLogs.retrievalResult')}</span>
                            <span class="detail-value ${hasResults ? 'text-success' : 'text-muted'}">
                                ${hasResults ? (itemCount > 0 ? _t('retrievalLogs.foundCount', { count: itemCount }) : _t('retrievalLogs.foundUnknown')) : _t('retrievalLogs.noMatch')}
                            </span>
                        </div>
                    </div>
                    ${hasResults && log.retrievedItems && log.retrievedItems.length > 0 ? `
                        <div class="retrieval-log-items-preview">
                            <div class="retrieval-log-items-label">${_t('retrievalLogs.retrievedItemsLabel')}</div>
                            <div class="retrieval-log-items-list">
                                ${log.retrievedItems.slice(0, 3).map((itemId, idx) => `
                                    <span class="retrieval-log-item-tag">${idx + 1}</span>
                                `).join('')}
                                ${log.retrievedItems.length > 3 ? `<span class="retrieval-log-item-tag more">+${log.retrievedItems.length - 3}</span>` : ''}
                            </div>
                        </div>
                    ` : ''}
                    <div class="retrieval-log-actions">
                        <button class="btn-secondary btn-sm" onclick="showRetrievalLogDetails(${index})" style="margin-top: 12px; display: inline-flex; align-items: center; gap: 4px;">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                                <circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                            </svg>
                            ${_t('retrievalLogs.viewDetails')}
                        </button>
                        <button class="btn-secondary btn-sm retrieval-log-delete-btn" onclick="deleteRetrievalLog('${escapeHtml(log.id)}', ${index})" style="margin-top: 12px; margin-left: 8px; display: inline-flex; align-items: center; gap: 4px; color: var(--error-color, #dc3545); border-color: var(--error-color, #dc3545);" onmouseover="this.style.backgroundColor='rgba(220, 53, 69, 0.1)'; this.style.color='#dc3545';" onmouseout="this.style.backgroundColor=''; this.style.color='var(--error-color, #dc3545)';" title="${_t('common.delete')}">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                                <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                            </svg>
                            ${_t('common.delete')}
                        </button>
                    </div>
                </div>
            </div>
        `;
    }).join('');
}

// Update retrieval statistics
function updateRetrievalStats(logs) {
    const statsContainer = document.getElementById('retrieval-stats');
    if (!statsContainer) return;
    
    const totalLogs = logs.length;
    // Determine whether there are results:check the retrievedItems array,length after filtering special markers>0,or contains a special marker
    const successfulLogs = logs.filter(log => {
        if (!log.retrievedItems) return false;
        if (Array.isArray(log.retrievedItems)) {
            const realItems = log.retrievedItems.filter(id => id !== '_has_results');
            return realItems.length > 0 || log.retrievedItems.includes('_has_results');
        }
        return false;
    }).length;
    // Calculate total knowledge item count (count only real IDs,exclude special markers)
    const totalItems = logs.reduce((sum, log) => {
        if (!log.retrievedItems) return sum;
        if (Array.isArray(log.retrievedItems)) {
            const realItems = log.retrievedItems.filter(id => id !== '_has_results');
            return sum + realItems.length;
        }
        return sum;
    }, 0);
    const successRate = totalLogs > 0 ? ((successfulLogs / totalLogs) * 100).toFixed(1) : 0;
    
    statsContainer.innerHTML = `
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.totalRetrievals">Total retrievals</span>
            <span class="retrieval-stat-value">${totalLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRetrievals">Successful retrievals</span>
            <span class="retrieval-stat-value text-success">${successfulLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRate">Success rate</span>
            <span class="retrieval-stat-value">${successRate}%</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.retrievedItems">Retrieved knowledge items</span>
            <span class="retrieval-stat-value">${totalItems}</span>
        </div>
    `;
    if (typeof window.applyTranslations === 'function') {
        window.applyTranslations(statsContainer);
    }
}

// Get relative time
function getTimeAgo(timeStr) {
    if (!timeStr) return '';
    
    // Process time string,supports multiple formats
    let date;
    if (typeof timeStr === 'string') {
        // First try direct parsing (supports RFC3339/ISO8601 formats)
        date = new Date(timeStr);
        
        // If parsing fails,try other formats
        if (isNaN(date.getTime())) {
            // SQLiteformat: "2006-01-02 15:04:05" or with a timezone
            const sqliteMatch = timeStr.match(/(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[+-]\d{2}:\d{2}|Z)?)/);
            if (sqliteMatch) {
                let timeStr2 = sqliteMatch[1].replace(' ', 'T');
                // If there is no timezone information,add Z to indicate UTC
                if (!timeStr2.includes('Z') && !timeStr2.match(/[+-]\d{2}:\d{2}$/)) {
                    timeStr2 += 'Z';
                }
                date = new Date(timeStr2);
            }
        }
        
        // If it still fails,try a more permissive format
        if (isNaN(date.getTime())) {
            // Try to match "YYYY-MM-DD HH:MM:SS" format
            const match = timeStr.match(/(\d{4})-(\d{2})-(\d{2})[\sT](\d{2}):(\d{2}):(\d{2})/);
            if (match) {
                date = new Date(
                    parseInt(match[1]), 
                    parseInt(match[2]) - 1, 
                    parseInt(match[3]),
                    parseInt(match[4]),
                    parseInt(match[5]),
                    parseInt(match[6])
                );
            }
        }
    } else {
        date = new Date(timeStr);
    }
    
    // Check whether the date is valid
    if (isNaN(date.getTime())) {
        return formatTime(timeStr);
    }
    
    // Check whether the date is reasonable (not before 1970,not too far in the future)
    const year = date.getFullYear();
    if (year < 1970 || year > 2100) {
        return formatTime(timeStr);
    }
    
    const now = new Date();
    const diff = now - date;
    
    // If the time difference is negative or too large (may be a parse error),return formatted time
    if (diff < 0 || diff > 365 * 24 * 60 * 60 * 1000 * 10) { // treat more than 10 years as an error
        return formatTime(timeStr);
    }
    
    const seconds = Math.floor(diff / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    
    if (days > 0) return `${days} days ago`;
    if (hours > 0) return `${hours} hours ago`;
    if (minutes > 0) return `${minutes} minutes ago`;
    return 'just now';
}

// Truncate ID display
function truncateId(id) {
    if (!id || id.length <= 16) return id;
    return id.substring(0, 8) + '...' + id.substring(id.length - 8);
}

// Filter retrieval logs
function filterRetrievalLogs() {
    const conversationId = document.getElementById('retrieval-logs-conversation-id').value.trim();
    const messageId = document.getElementById('retrieval-logs-message-id').value.trim();
    loadRetrievalLogs(conversationId, messageId);
}

// Refresh retrieval logs
function refreshRetrievalLogs() {
    filterRetrievalLogs();
}

// Delete retrieval log
async function deleteRetrievalLog(id, index) {
    if (!confirm(_t('retrievalLogs.deleteConfirm'))) {
        return;
    }
    
    // Find the log card and delete button to delete
    const logCard = document.querySelector(`.retrieval-log-card[data-index="${index}"]`);
    const deleteButton = logCard ? logCard.querySelector('.retrieval-log-delete-btn') : null;
    let originalButtonOpacity = '';
    let originalButtonDisabled = false;
    
    // Set delete button loading state
    if (deleteButton) {
        originalButtonOpacity = deleteButton.style.opacity;
        originalButtonDisabled = deleteButton.disabled;
        deleteButton.style.opacity = '0.5';
        deleteButton.style.cursor = 'not-allowed';
        deleteButton.disabled = true;
        
        // Add loading animation
        const svg = deleteButton.querySelector('svg');
        if (svg) {
            svg.style.animation = 'spin 1s linear infinite';
        }
    }
    
    // Remove the item from the UI immediately (optimistic update)
    if (logCard) {
        logCard.style.transition = 'opacity 0.3s ease-out, transform 0.3s ease-out';
        logCard.style.opacity = '0';
        logCard.style.transform = 'translateX(-20px)';
        
        // Remove after the animation completes
        setTimeout(() => {
            if (logCard.parentElement) {
                logCard.remove();
                
                // Update statistics (temporary update; data reloads shortly)
                updateRetrievalStatsAfterDelete();
            }
        }, 300);
    }
    
    try {
        const response = await apiFetch(`/api/knowledge/retrieval-logs/${id}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({}));
            throw new Error(errorData.error || 'Failed to delete retrieval log');
        }
        
        // Show success notification
        showNotification('✅ Deleted successfully! The retrieval record was removed from the system.', 'success');
        
        // Remove the item from memory
        if (retrievalLogsData && index >= 0 && index < retrievalLogsData.length) {
            retrievalLogsData.splice(index, 1);
        }
        
        // Reload data to ensure synchronization
        const conversationId = document.getElementById('retrieval-logs-conversation-id')?.value.trim() || '';
        const messageId = document.getElementById('retrieval-logs-message-id')?.value.trim() || '';
        await loadRetrievalLogs(conversationId, messageId);
        
    } catch (error) {
        console.error('Failed to delete retrieval log:', error);
        
        // If deletion fails,restore the item display
        if (logCard) {
            logCard.style.opacity = '1';
            logCard.style.transform = '';
            logCard.style.transition = '';
        }
        
        // Restore delete button state
        if (deleteButton) {
            deleteButton.style.opacity = originalButtonOpacity || '';
            deleteButton.style.cursor = '';
            deleteButton.disabled = originalButtonDisabled;
            const svg = deleteButton.querySelector('svg');
            if (svg) {
                svg.style.animation = '';
            }
        }
        
        showNotification(_t('retrievalLogs.deleteError') + ': ' + error.message, 'error');
    }
}

// Temporarily update statistics (after delete)
function updateRetrievalStatsAfterDelete() {
    const statsContainer = document.getElementById('retrieval-stats');
    if (!statsContainer) return;
    
    const allLogs = document.querySelectorAll('.retrieval-log-card');
    const totalLogs = allLogs.length;
    
    // Calculate successful retrieval count
    const successfulLogs = Array.from(allLogs).filter(card => {
        return card.classList.contains('has-results');
    }).length;
    
    // Calculate total knowledge item count (simplified here,should actually be fetched from the server)
    const totalItems = Array.from(allLogs).reduce((sum, card) => {
        const badge = card.querySelector('.retrieval-log-result-badge');
        if (badge && badge.classList.contains('success')) {
            const text = badge.textContent.trim();
            const match = text.match(/(\d+)/);
            if (match) {
                return sum + parseInt(match[1], 10);
            }
            return sum + 1; // Results exist but count is unknown (such as "Has results" / "Has results")
        }
        return sum;
    }, 0);
    
    const successRate = totalLogs > 0 ? ((successfulLogs / totalLogs) * 100).toFixed(1) : 0;
    
    statsContainer.innerHTML = `
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.totalRetrievals">Total retrievals</span>
            <span class="retrieval-stat-value">${totalLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRetrievals">Successful retrievals</span>
            <span class="retrieval-stat-value text-success">${successfulLogs}</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.successRate">Success rate</span>
            <span class="retrieval-stat-value">${successRate}%</span>
        </div>
        <div class="retrieval-stat-item">
            <span class="retrieval-stat-label" data-i18n="retrievalLogs.retrievedItems">Retrieved knowledge items</span>
            <span class="retrieval-stat-value">${totalItems}</span>
        </div>
    `;
    if (typeof window.applyTranslations === 'function') {
        window.applyTranslations(statsContainer);
    }
}

// Show retrieval log details
async function showRetrievalLogDetails(index) {
    if (!retrievalLogsData || index < 0 || index >= retrievalLogsData.length) {
        showNotification(_t('retrievalLogs.detailError'), 'error');
        return;
    }
    
    const log = retrievalLogsData[index];
    
    // Get retrieved knowledge item details
    let retrievedItemsDetails = [];
    if (log.retrievedItems && Array.isArray(log.retrievedItems)) {
        const realItemIds = log.retrievedItems.filter(id => id !== '_has_results');
        if (realItemIds.length > 0) {
            try {
                // Batch get knowledge item details
                const itemPromises = realItemIds.map(async (itemId) => {
                    try {
                        const response = await apiFetch(`/api/knowledge/items/${itemId}`);
                        if (response.ok) {
                            return await response.json();
                        }
                        return null;
                    } catch (err) {
                        console.error(`Get knowledge item ${itemId} failed:`, err);
                        return null;
                    }
                });
                
                const items = await Promise.all(itemPromises);
                retrievedItemsDetails = items.filter(item => item !== null);
            } catch (err) {
                console.error('Failed to batch get knowledge item details:', err);
            }
        }
    }
    
    // Show details modal
    showRetrievalLogDetailsModal(log, retrievedItemsDetails);
}

// Show retrieval log detailsmodal
function showRetrievalLogDetailsModal(log, retrievedItems) {
    // Create or get modal
    let modal = document.getElementById('retrieval-log-details-modal');
    if (!modal) {
        modal = document.createElement('div');
        modal.id = 'retrieval-log-details-modal';
        modal.className = 'modal';
        modal.innerHTML = `
            <div class="modal-content" style="max-width: 900px; max-height: 90vh; overflow-y: auto;">
                <div class="modal-header">
                    <h2 data-i18n="retrievalLogs.detailsTitle">Retrieval details</h2>
                    <span class="modal-close" onclick="closeRetrievalLogDetailsModal()">&times;</span>
                </div>
                <div class="modal-body" id="retrieval-log-details-content">
                </div>
                <div class="modal-footer">
                    <button class="btn-secondary" onclick="closeRetrievalLogDetailsModal()" data-i18n="common.close">close</button>
                </div>
            </div>
        `;
        if (typeof window.applyTranslations === 'function') {
            window.applyTranslations(modal);
        }
        document.body.appendChild(modal);
    }
    
    // Fill content
    const content = document.getElementById('retrieval-log-details-content');
    const timeAgo = getTimeAgo(log.createdAt);
    const fullTime = formatTime(log.createdAt);
    
    let itemsHtml = '';
    if (retrievedItems.length > 0) {
        itemsHtml = retrievedItems.map((item, idx) => {
            // Extract content preview
            let preview = item.content || '';
            preview = preview.replace(/^#+\s+/gm, '');
            preview = preview.replace(/```[\s\S]*?```/g, '');
            preview = preview.replace(/`[^`]+`/g, '');
            preview = preview.replace(/\[([^\]]+)\]\([^\)]+\)/g, '$1');
            preview = preview.replace(/\n+/g, ' ').replace(/\s+/g, ' ').trim();
            const previewText = preview.length > 200 ? preview.substring(0, 200) + '...' : preview;
            
            return `
                <div class="retrieval-detail-item-card" style="margin-bottom: 16px; padding: 16px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-secondary);">
                    <div style="display: flex; justify-content: space-between; align-items: start; margin-bottom: 8px;">
                        <h4 style="margin: 0; color: var(--text-primary);">${idx + 1}. ${escapeHtml(item.title || _t('retrievalLogs.untitled'))}</h4>
                        <span style="font-size: 0.875rem; color: var(--text-secondary);">${escapeHtml(item.category || _t('retrievalLogs.uncategorized'))}</span>
                    </div>
                    ${item.filePath ? `<div style="font-size: 0.875rem; color: var(--text-muted); margin-bottom: 8px;">📁 ${escapeHtml(item.filePath)}</div>` : ''}
                    <div style="font-size: 0.875rem; color: var(--text-secondary); line-height: 1.6;">
                        ${escapeHtml(previewText || _t('retrievalLogs.noContentPreview'))}
                    </div>
                </div>
            `;
        }).join('');
    } else {
        itemsHtml = '<div style="padding: 16px; text-align: center; color: var(--text-muted);">' + _t('retrievalLogs.noItemDetails') + '</div>';
    }
    
    content.innerHTML = `
        <div style="display: flex; flex-direction: column; gap: 20px;">
            <div class="retrieval-detail-section">
                <h3 style="margin: 0 0 12px 0; font-size: 1.125rem; color: var(--text-primary);">${_t('retrievalLogs.queryInfo')}</h3>
                <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px; border-left: 3px solid var(--accent-color);">
                    <div style="font-weight: 500; margin-bottom: 8px; color: var(--text-primary);">${_t('retrievalLogs.queryContent')}</div>
                    <div style="color: var(--text-primary); line-height: 1.6; word-break: break-word;">${escapeHtml(log.query || _t('retrievalLogs.noQuery'))}</div>
                </div>
            </div>
            
            <div class="retrieval-detail-section">
                <h3 style="margin: 0 0 12px 0; font-size: 1.125rem; color: var(--text-primary);">${_t('retrievalLogs.retrievalInfo')}</h3>
                <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px;">
                    ${log.riskType ? `
                        <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                            <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.riskType')}</div>
                            <div style="font-weight: 500; color: var(--text-primary);">${escapeHtml(log.riskType)}</div>
                        </div>
                    ` : ''}
                    <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                        <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.retrievalTime')}</div>
                        <div style="font-weight: 500; color: var(--text-primary);" title="${fullTime}">${timeAgo}</div>
                    </div>
                    <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                        <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.retrievalResult')}</div>
                        <div style="font-weight: 500; color: var(--text-primary);">${_t('retrievalLogs.itemsCount', { count: retrievedItems.length })}</div>
                    </div>
                </div>
            </div>
            
            ${log.conversationId || log.messageId ? `
                <div class="retrieval-detail-section">
                    <h3 style="margin: 0 0 12px 0; font-size: 1.125rem; color: var(--text-primary);">${_t('retrievalLogs.relatedInfo')}</h3>
                    <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 12px;">
                        ${log.conversationId ? `
                            <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                                <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.conversationId')}</div>
                                <code style="font-size: 0.8125rem; color: var(--text-primary); word-break: break-all; cursor: pointer;"
                                      onclick="navigator.clipboard.writeText('${escapeHtml(log.conversationId)}'); this.title='Copied!'; setTimeout(() => this.title='Click to copy', 2000);"
                                      title="Click to copy">${escapeHtml(log.conversationId)}</code>
                            </div>
                        ` : ''}
                        ${log.messageId ? `
                            <div style="padding: 12px; background: var(--bg-secondary); border-radius: 6px;">
                                <div style="font-size: 0.875rem; color: var(--text-secondary); margin-bottom: 4px;">${_t('retrievalLogs.messageId')}</div>
                                <code style="font-size: 0.8125rem; color: var(--text-primary); word-break: break-all; cursor: pointer;"
                                      onclick="navigator.clipboard.writeText('${escapeHtml(log.messageId)}'); this.title='Copied!'; setTimeout(() => this.title='Click to copy', 2000);"
                                      title="Click to copy">${escapeHtml(log.messageId)}</code>
                            </div>
                        ` : ''}
                    </div>
                </div>
            ` : ''}
            
            <div class="retrieval-detail-section">
                <h3 style="margin: 0 0 12px 0; font-size: 1.125rem; color: var(--text-primary);">Retrieved knowledge items (${retrievedItems.length})</h3>
                ${itemsHtml}
            </div>
        </div>
    `;
    
    modal.style.display = 'block';
}

// Close retrieval log details modal
function closeRetrievalLogDetailsModal() {
    const modal = document.getElementById('retrieval-log-details-modal');
    if (modal) {
        modal.style.display = 'none';
    }
}

// Click outside the modal to close
window.addEventListener('click', function(event) {
    const modal = document.getElementById('retrieval-log-details-modal');
    if (event.target === modal) {
        closeRetrievalLogDetailsModal();
    }
});

// Re-render retrieval history and statistics when language changes,so dynamic content updates with the language;The knowledge management page"disabled"block already uses data-i18n,will be updated by applyTranslations(document) automatically
document.addEventListener('languagechange', function () {
    var cur = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || '');
    if (cur === 'knowledge-retrieval-logs') {
        if (retrievalLogsData && retrievalLogsData.length >= 0) {
            renderRetrievalLogs(retrievalLogsData);
        }
    } else if (cur === 'knowledge-management') {
        // Only for"knowledge base disabled"status:already has data-i18n,applyTranslations has already handled it;reapply here optionally for old DOM compatibility
        var listEl = document.getElementById('knowledge-items-list');
        if (listEl && typeof window.applyTranslations === 'function') {
            window.applyTranslations(listEl);
        }
    }
});

// Load data when switching pages
if (typeof switchPage === 'function') {
    const originalSwitchPage = switchPage;
    window.switchPage = function(page) {
        originalSwitchPage(page);
        
        if (page === 'knowledge-management') {
            loadKnowledgeCategories();
            loadKnowledgeItems(knowledgePagination.currentCategory, 1, knowledgePagination.pageSize);
            updateIndexProgress(); // Update index progress
        } else if (page === 'knowledge-retrieval-logs') {
            loadRetrievalLogs();
            // Stop polling when switching to another page
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
        } else {
            // Stop polling when switching to another page
            if (indexProgressInterval) {
                clearInterval(indexProgressInterval);
                indexProgressInterval = null;
            }
        }
    };
}

// Clean up timers on page unload
window.addEventListener('beforeunload', function() {
    if (indexProgressInterval) {
        clearInterval(indexProgressInterval);
        indexProgressInterval = null;
    }
});

// Utility functions
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function formatTime(timeStr) {
    if (!timeStr) return '';
    
    // Process time string,supports multiple formats
    let date;
    if (typeof timeStr === 'string') {
        // First try direct parsing (supports RFC3339/ISO8601 formats)
        date = new Date(timeStr);
        
        // If parsing fails,try other formats
        if (isNaN(date.getTime())) {
            // SQLiteformat: "2006-01-02 15:04:05" or with a timezone
            const sqliteMatch = timeStr.match(/(\d{4}-\d{2}-\d{2}[\sT]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[+-]\d{2}:\d{2}|Z)?)/);
            if (sqliteMatch) {
                let timeStr2 = sqliteMatch[1].replace(' ', 'T');
                // If there is no timezone information,add Z to indicate UTC
                if (!timeStr2.includes('Z') && !timeStr2.match(/[+-]\d{2}:\d{2}$/)) {
                    timeStr2 += 'Z';
                }
                date = new Date(timeStr2);
            }
        }
        
        // If it still fails,try a more permissive format
        if (isNaN(date.getTime())) {
            // Try to match "YYYY-MM-DD HH:MM:SS" format
            const match = timeStr.match(/(\d{4})-(\d{2})-(\d{2})[\sT](\d{2}):(\d{2}):(\d{2})/);
            if (match) {
                date = new Date(
                    parseInt(match[1]), 
                    parseInt(match[2]) - 1, 
                    parseInt(match[3]),
                    parseInt(match[4]),
                    parseInt(match[5]),
                    parseInt(match[6])
                );
            }
        }
    } else {
        date = new Date(timeStr);
    }
    
    // ifdate is invalid,check whether it is a zero time
    if (isNaN(date.getTime())) {
        // Check whether this is the string form of zero time
        if (typeof timeStr === 'string' && (timeStr.includes('0001-01-01') || timeStr.startsWith('0001'))) {
            return '';
        }
        console.warn('Unable to parse time:', timeStr);
        return '';
    }
    
    // Check whether the date is reasonable (not before 1970,not too far in the future)
    const year = date.getFullYear();
    if (year < 1970 || year > 2100) {
        // If this is zero time (0001-01-01),return an empty string,do not display
        if (year === 1) {
            return '';
        }
        console.warn('Time value is unreasonable:', timeStr, 'parsed as:', date);
        return '';
    }
    
    return date.toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
    });
}

// Show notification
function showNotification(message, type = 'info') {
    // if presentglobal notification system (and is not the current function),use it
    if (typeof window.showNotification === 'function' && window.showNotification !== showNotification) {
        window.showNotification(message, type);
        return;
    }
    
    // otherwiseusecustom toast notification
    showToastNotification(message, type);
}

// Show toast notification
function showToastNotification(message, type = 'info') {
    // Create notification container (ifdoes not exist)
    let container = document.getElementById('toast-notification-container');
    if (!container) {
        container = document.createElement('div');
        container.id = 'toast-notification-container';
        container.style.cssText = `
            position: fixed;
            top: 20px;
            right: 20px;
            z-index: 10100;
            display: flex;
            flex-direction: column;
            gap: 12px;
            pointer-events: none;
        `;
        document.body.appendChild(container);
    }
    
    // Create notification element
    const toast = document.createElement('div');
    toast.className = `toast-notification toast-${type}`;
    
    const typeStyles = {
        success: {
            background: '#f4fbf6',
            border: '1px solid #cce8d4',
            color: '#3d6654',
            iconColor: '#52a06a',
            icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true"><circle cx="8" cy="8" r="7" stroke="currentColor" stroke-width="1.2"/><path d="M5 8l2 2 4-4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>'
        },
        error: {
            background: '#fef7f7',
            border: '1px solid #f3d0d0',
            color: '#8b4444',
            iconColor: '#c96a6a',
            icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true"><circle cx="8" cy="8" r="7" stroke="currentColor" stroke-width="1.2"/><path d="M6 6l4 4M10 6l-4 4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>'
        },
        info: {
            background: '#f5f9ff',
            border: '1px solid #cfe0f5',
            color: '#4a6078',
            iconColor: '#6b8fbf',
            icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true"><circle cx="8" cy="8" r="7" stroke="currentColor" stroke-width="1.2"/><path d="M8 7v4M8 5.5v.01" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>'
        },
        warning: {
            background: '#fffbf3',
            border: '1px solid #f0dfc0',
            color: '#7a6535',
            iconColor: '#c4a04a',
            icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true"><path d="M8 2.5l6 10.5H2L8 2.5z" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round"/><path d="M8 7v2.5M8 11v.01" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>'
        }
    };

    const style = typeStyles[type] || typeStyles.info;

    toast.style.cssText = `
        background: ${style.background};
        border: ${style.border};
        color: ${style.color};
        padding: 10px 14px;
        border-radius: 10px;
        box-shadow: 0 2px 8px rgba(15, 23, 42, 0.06);
        min-width: 220px;
        max-width: 420px;
        pointer-events: auto;
        animation: slideInRight 0.25s ease-out;
        display: flex;
        align-items: center;
        gap: 10px;
        font-size: 0.875rem;
        line-height: 1.45;
        word-wrap: break-word;
        backdrop-filter: blur(8px);
    `;

    toast.innerHTML = `
        <span style="color: ${style.iconColor}; flex-shrink: 0; display: flex; align-items: center;">${style.icon}</span>
        <span style="flex: 1;">${escapeHtml(message)}</span>
        <button onclick="this.parentElement.remove()" style="
            background: transparent;
            border: none;
            color: ${style.color};
            cursor: pointer;
            font-size: 1rem;
            padding: 0;
            margin-left: 4px;
            opacity: 0.45;
            flex-shrink: 0;
            width: 20px;
            height: 20px;
            display: flex;
            align-items: center;
            justify-content: center;
        " onmouseover="this.style.opacity='0.75'" onmouseout="this.style.opacity='0.45'">×</button>
    `;
    
    container.appendChild(toast);
    
    // Automatically remove (success messages show for 5 seconds,error messages show for 7 seconds,others show for 4 seconds)
    const duration = type === 'success' ? 5000 : type === 'error' ? 7000 : 4000;
    setTimeout(() => {
        if (toast.parentElement) {
            toast.style.animation = 'slideOutRight 0.3s ease-out';
            setTimeout(() => {
                if (toast.parentElement) {
                    toast.remove();
                }
            }, 300);
        }
    }, duration);
}

// Add CSS animation (ifdoes not exist)
if (!document.getElementById('toast-notification-styles')) {
    const style = document.createElement('style');
    style.id = 'toast-notification-styles';
    style.textContent = `
        @keyframes slideInRight {
            from {
                transform: translateX(16px);
                opacity: 0;
            }
            to {
                transform: translateX(0);
                opacity: 1;
            }
        }
        @keyframes slideOutRight {
            from {
                transform: translateX(0);
                opacity: 1;
            }
            to {
                transform: translateX(16px);
                opacity: 0;
            }
        }
    `;
    document.head.appendChild(style);
}

// Click outside the modal to close
window.addEventListener('click', function(event) {
    const modal = document.getElementById('knowledge-item-modal');
    if (event.target === modal) {
        closeKnowledgeItemModal();
    }
});

// Switch to settings page (Used for feature-disabled notices)
function switchToSettings() {
    if (typeof switchPage === 'function') {
        switchPage('settings');
        // After settings page loads,switch to knowledge base config section
        setTimeout(() => {
            if (typeof switchSettingsSection === 'function') {
                // Find knowledge base config section (usually in basic settings)
                const knowledgeSection = document.querySelector('[data-section="knowledge"]');
                if (knowledgeSection) {
                    switchSettingsSection('knowledge');
                } else {
                    // ifthere is no standalone knowledge base section,switch to basic settings
                    switchSettingsSection('basic');
                    // Scroll to knowledge base config area
                    setTimeout(() => {
                        const knowledgeEnabledCheckbox = document.getElementById('knowledge-enabled');
                        if (knowledgeEnabledCheckbox) {
                            knowledgeEnabledCheckbox.scrollIntoView({ behavior: 'smooth', block: 'center' });
                            // Highlight
                            knowledgeEnabledCheckbox.parentElement.style.transition = 'background-color 0.3s';
                            knowledgeEnabledCheckbox.parentElement.style.backgroundColor = '#e3f2fd';
                            setTimeout(() => {
                                knowledgeEnabledCheckbox.parentElement.style.backgroundColor = '';
                            }, 2000);
                        }
                    }, 300);
                }
            }
        }, 100);
    }
}

// Custom dropdown component interactions
document.addEventListener('DOMContentLoaded', function() {
    const wrapper = document.getElementById('knowledge-category-filter-wrapper');
    const trigger = document.getElementById('knowledge-category-filter-trigger');
    
    if (wrapper && trigger) {
        // Click trigger to open/closedropdown menu
        trigger.addEventListener('click', function(e) {
            e.stopPropagation();
            wrapper.classList.toggle('open');
        });
        
        // Click outsideclosedropdown menu
        document.addEventListener('click', function(e) {
            if (!wrapper.contains(e.target)) {
                wrapper.classList.remove('open');
            }
        });
        
        // Update selected state when choosing an option
        const dropdown = document.getElementById('knowledge-category-filter-dropdown');
        if (dropdown) {
            // Defaultselected"all"option
            const defaultOption = dropdown.querySelector('.custom-select-option[data-value=""]');
            if (defaultOption) {
                defaultOption.classList.add('selected');
            }
            
            dropdown.addEventListener('click', function(e) {
                const option = e.target.closest('.custom-select-option');
                if (option) {
                    // Remove previous selected state
                    dropdown.querySelectorAll('.custom-select-option').forEach(opt => {
                        opt.classList.remove('selected');
                    });
                    // Add selected state
                    option.classList.add('selected');
                }
            });
        }
    }
});

