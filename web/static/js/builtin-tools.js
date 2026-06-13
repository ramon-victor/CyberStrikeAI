/**
 * Builtin tool name constants
 * All places in the frontend code that use builtin tool names should use these constants instead of hardcoded strings
 *
 * Note: these constants must be consistent with the constants in the backend's internal/mcp/builtin/constants.go
 */

// Builtin tool name constants
const BuiltinTools = {
    // Vulnerability management tools
    RECORD_VULNERABILITY: 'record_vulnerability',
    
    // Knowledge base tools
    LIST_KNOWLEDGE_RISK_TYPES: 'list_knowledge_risk_types',
    SEARCH_KNOWLEDGE_BASE: 'search_knowledge_base'
};

// Check if it is a builtin tool
function isBuiltinTool(toolName) {
    return Object.values(BuiltinTools).includes(toolName);
}

// Get a list of all builtin tool names
function getAllBuiltinTools() {
    return Object.values(BuiltinTools);
}

