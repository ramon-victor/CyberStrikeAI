import re

replacements = {
    "web/static/js/monitor.js": [
        (r'/\*\* 监控页展示：内部 mcp::tool → 模型侧 mcp__tool \*/', r'/** Monitor page display: internal mcp::tool → model-side mcp__tool */'),
        (r'/\*\* 筛选/API：mcp__tool → 内部 mcp::tool（与库存一致） \*/', r'/** Filter/API: mcp__tool → internal mcp::tool (matches inventory) */'),
        (r'/\*\* 流式纯文本 DOM：按帧合并更新，尽量增量 appendData，避免每条 SSE 全量 textContent 阻塞主线程 \*/', r'/** Streaming plain text DOM: merge updates by frame, try to increment with appendData, avoid full textContent per SSE blocking the main thread */'),
        (r'/\*\* 跟踪仍有待刷新的流式节点，便于快照时间线前一次性 flush \*/', r'/** Track streaming nodes that still need to be refreshed, so they can be flushed all at once before snapshotting the timeline */'),
        (r'\* 分批处理 SSE data 行并在批间让出主线程，避免单次 read\(\) 内数百条事件连续阻塞 UI。', r'* Process SSE data lines in batches and yield the main thread between batches to avoid blocking the UI with hundreds of events in a single read().'),
        (r"console\.error\('解析事件数据失败:', e, line\);", r"console.error('Failed to parse event data:', e, line);"),
        (r'// 快照 innerHTML 前刷掉尚未执行的 rAF 流式更新，避免过程详情少最后几帧', r'// Flush pending rAF streaming updates before snapshotting innerHTML to avoid missing the last few frames in the process details'),
        (r'// 主通道进入新轮次后不复用上一轮的「执行输出」时间线条目', r'// After the main channel enters a new round, do not reuse the "execution output" timeline entry from the previous round'),
        (r'// 助手正文段结束、进入工具调用：下一段 response_start 应新建时间线条目', r'// Assistant text segment ends, enters tool call: the next response_start should create a new timeline entry'),
        (r": '🔁 自动续跑（无助手正文）';", r": '🔁 Auto-resume (No assistant text)';"),
        (r": '会话已结束但未捕获到助手正文，正在基于轨迹自动续跑…'\),", r": 'Session ended but assistant text not captured, auto-resuming based on trajectory...'),"),
        (r"throw new Error\(result\.error \|\| '获取监控数据失败'\);", r"throw new Error(result.error || 'Failed to fetch monitor data');"),
        (r"\|\| `\$\{time\}：\$\{total\} 次（失败 \$\{failed\}）`;", r"|| `${time}: ${total} calls (${failed} failed)`;"),
        (r"const cumulative = mcpMonitorT\('scopeCumulative'\) \|\| '累计';", r"const cumulative = mcpMonitorT('scopeCumulative') || 'Cumulative';"),
        (r"\|\| `该时段多数时间为 0，峰值 \$\{peak\} 次出现在 \$\{peakTime\}`;", r"|| `Mostly 0 during this period, peak ${peak} calls at ${peakTime}`;"),
        (r"throw new Error\(timelineJson\.error \|\| '加载趋势失败'\);", r"throw new Error(timelineJson.error || 'Failed to load trend');"),
        (r"monitorFallback\('全部工具合计', 'All tools combined'\)", r"monitorFallback('All tools combined', 'All tools combined')"),
        (r"monitorFallback\('无法加载调用趋势', 'Failed to load call trend'\)", r"monitorFallback('Failed to load call trend', 'Failed to load call trend')"),
        (r"\|\| `区间内 \$\{summaryTotal\} 次 · 峰值 \$\{peak\}`;", r"|| `${summaryTotal} calls in range · peak ${peak}`;"),
        (r"monitorFallback\('该时段暂无调用', 'No calls in this period'\)", r"monitorFallback('No calls in this period', 'No calls in this period')"),
        (r"const totalLegend = mcpMonitorT\('timelineTotalLegend'\) \|\| '总调用';", r"const totalLegend = mcpMonitorT('timelineTotalLegend') || 'Total Calls';"),
        (r"const failLegend = mcpMonitorT\('timelineFailedLegend'\) \|\| '失败';", r"const failLegend = mcpMonitorT('timelineFailedLegend') || 'Failed';"),
        (r"monitorFallback\('工具统计', 'Tool statistics'\)", r"monitorFallback('Tool statistics', 'Tool statistics')"),
        (r"monitorFallback\('调用趋势', 'Call trend'\)", r"monitorFallback('Call trend', 'Call trend')"),
        (r"monitorFallback\('点击色条或列表行筛选下方执行记录', 'Click a bar segment or row to filter records below'\)", r"monitorFallback('Click a bar segment or row to filter records below', 'Click a bar segment or row to filter records below')"),
        (r"\|\| `已筛选：\$\{filterChipLabel\}`", r"|| `Filtered: ${filterChipLabel}`"),
        (r"\|\| '清除工具筛选'", r"|| 'Clear tool filter'"),
        (r"const othersLabel = mcpMonitorT\('distOthers'\) \|\| '其他工具';", r"const othersLabel = mcpMonitorT('distOthers') || 'Other tools';"),
        (r"\|\| '占比'", r"|| 'Share'"),
        (r"monitorFallback\('总调用次数', 'Total calls'\)", r"monitorFallback('Total calls', 'Total calls')"),
        (r"monitorFallback\('成功率', 'Success rate'\)", r"monitorFallback('Success rate', 'Success rate')"),
        (r"monitorFallback\('最近一次调用', 'Last call'\)", r"monitorFallback('Last call', 'Last call')"),
        (r"monitorFallback\(`成功 \$\{totals\.success\}`", r"monitorFallback(`Success ${totals.success}`"),
        (r"monitorFallback\(`失败 \$\{totals\.failed\}`", r"monitorFallback(`Failed ${totals.failed}`"),
        (r"\|\| '工具'", r"|| 'Tool'"),
        (r"\|\| '调用'", r"|| 'Calls'"),
        (r"const colRate = mcpMonitorT\('columnSuccessRate'\) \|\| '成功率';", r"const colRate = mcpMonitorT('columnSuccessRate') || 'Success Rate';"),
        (r"\|\| '未知工具'", r"|| 'Unknown Tool'"),
        (r"\|\| `\$\{name\}，\$\{total\} 次调用，成功率 \$\{toolRate\}%`;", r"|| `${name}, ${total} calls, success rate ${toolRate}%`;"),
        (r"\|\| `失败 \$\{failed\}`", r"|| `Failed ${failed}`"),
        (r"/\*\* MCP 合并面板左侧：堆叠占比条 \+ 工具排行列表（无饼图/表格套娃） \*/", r"/** MCP combined panel left: stacked share bar + tool ranking list (no nested charts/tables) */"),
        (r"\|\| `Top \$\{MCP_STATS_TOP_N\} 占 \$\{topNSharePct\}% · 共 \$\{totals\.total\} 次`;", r"|| `Top ${MCP_STATS_TOP_N} share ${topNSharePct}% · ${totals.total} calls total`;"),
        (r"\|\| '调用分布'", r"|| 'Call Distribution'"),
        (r"\|\| `\$\{displayName\}，占 \$\{s\.pct\}%，\$\{s\.calls\} 次`;", r"|| `${displayName}, share ${s.pct}%, ${s.calls} calls`;"),
        (r"\|\| `\$\{name\}，\$\{total\} 次，成功率 \$\{toolRate\}%`;", r"|| `${name}, ${total} calls, success rate ${toolRate}%`;"),
        (r"\|\| '累计'", r"|| 'Cumulative'"),
        (r"\|\| '点击扇区筛选'", r"|| 'Click sector to filter'"),
        (r"\|\| `Top \$\{MCP_STATS_TOP_N\} 占全部调用`;", r"|| `Top ${MCP_STATS_TOP_N} of total calls`;"),
        (r"\|\| `\$\{s\.name\}，占 \$\{s\.pct\}%，\$\{s\.calls\} 次`\);", r"|| `${s.name}, share ${s.pct}%, ${s.calls} calls`);"),
        (r"/\*\* @deprecated 保留供其他页面；MCP 监控主面板请用 renderMcpStatsToolTable \*/", r"/** @deprecated Retained for other pages; MCP monitor main panel should use renderMcpStatsToolTable */"),
        (r"monitorFallback\('在对话或任务中调用 MCP 工具后，执行记录将显示在此处',", r"monitorFallback('Execution records will appear here after you invoke MCP tools in chat or tasks',")
    ],
    "web/static/js/notifications.js": [
        (r"console\.warn\('读取通知已读时间失败:', e\);", r"console.warn('Failed to read notification read time:', e);"),
        (r"console\.warn\('保存通知已读时间失败:', e\);", r"console.warn('Failed to save notification read time:', e);"),
        (r"t\('notifications\.empty', '暂无新事件'\)", r"t('notifications.empty', 'No new events')"),
        (r"t\('notifications\.itemDefaultTitle', '通知'\)", r"t('notifications.itemDefaultTitle', 'Notification')"),
        (r"t\('common\.view', '查看'\)", r"t('common.view', 'View')"),
        (r"t\('notifications\.markSingleRead', '已读'\)", r"t('notifications.markSingleRead', 'Read')"),
        (r"console\.warn\('刷新通知失败:', e\);", r"console.warn('Failed to refresh notifications:', e);"),
        (r"// 从仪表盘「查看全部」等容器外入口打开时，同一 click 会冒泡到 document，", r"// When opened from an external entry like 'View All' on the dashboard, the same click bubbles up to document,"),
        (r"// handleDocumentClick 会误判为「点在外面」并立刻关掉。推迟到宏任务再展开即可。", r"// handleDocumentClick will mistakenly judge it as 'clicking outside' and close it immediately. Delaying to a macro task to expand will suffice.")
    ],
    "web/static/js/projects.js": [
        (r"// 服务端 total 明确大于当前页末尾 → 直接信任", r"// Server total is clearly greater than the end of the current page → trust directly"),
        (r"// 不足一页 → 已是最后一页", r"// Less than a page → already the last page"),
        (r"// 满页但 total 可能被误算为 items\.length → 探测下一页", r"// Full page but total might be miscalculated as items.length → probe the next page")
    ]
}

for filepath, file_replacements in replacements.items():
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()
    
    for old, new in file_replacements:
        content = re.sub(old, new, content)
        
    with open(filepath, 'w', encoding='utf-8') as f:
        f.write(content)

print("Done")
