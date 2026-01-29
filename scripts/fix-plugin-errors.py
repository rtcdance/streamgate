#!/usr/bin/env python3
"""
Fix plugin errors: remove security package references and fix logger calls
"""

import re
import sys
from pathlib import Path

def fix_file(filepath):
    """Fix a single file"""
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # Remove security imports
    content = re.sub(r'\s*"streamgate/pkg/security"\n', '', content)
    
    # Remove rateLimiter and auditLogger fields from struct
    content = re.sub(r'\s*rateLimiter\s+\*security\.RateLimiter\n', '', content)
    content = re.sub(r'\s*auditLogger\s+\*security\.AuditLogger\n', '', content)
    
    # Remove rateLimiter and auditLogger initialization
    content = re.sub(r'\s*rateLimiter:\s+security\.NewRateLimiter\([^)]+\),\n', '', content)
    content = re.sub(r'\s*auditLogger:\s+security\.NewAuditLogger\([^)]+\),\n', '', content)
    
    # Remove rate limit checks (multi-line)
    content = re.sub(
        r'\t// Check rate limit\n\tif !h\.rateLimiter\.Allow\([^)]+\) \{\n'
        r'\t\th\.metricsCollector\.IncrementCounter\([^)]+\)\n'
        r'\t\tw\.WriteHeader\(http\.StatusTooManyRequests\)\n'
        r'\t\tjson\.NewEncoder\(w\)\.Encode\([^)]+\)\n'
        r'\t\treturn\n'
        r'\t\}\n\n',
        '',
        content
    )
    
    # Fix logger calls - wrap string literals with zap.String()
    # Pattern: logger.Info("message", "key", value)
    content = re.sub(
        r'(logger\.(?:Info|Error|Warn|Debug))\(([^,]+),\s*"([^"]+)",\s*([^)]+)\)',
        r'\1(\2, zap.String("\3", \4))',
        content
    )
    
    # Fix logger calls with multiple key-value pairs
    content = re.sub(
        r'(h\.logger\.(?:Info|Error|Warn|Debug))\(([^,]+),\s*"([^"]+)",\s*([^,]+),\s*"([^"]+)",\s*([^)]+)\)',
        r'\1(\2, zap.String("\3", \4), zap.String("\5", \6))',
        content
    )
    
    if content != original:
        with open(filepath, 'w') as f:
            f.write(content)
        print(f"Fixed: {filepath}")
        return True
    return False

def main():
    # Fix all plugin handler files
    plugin_dirs = [
        'pkg/plugins/api',
        'pkg/plugins/auth',
        'pkg/plugins/cache',
        'pkg/plugins/metadata',
        'pkg/plugins/monitor',
        'pkg/plugins/streaming',
        'pkg/plugins/transcoder',
        'pkg/plugins/upload',
        'pkg/plugins/worker',
    ]
    
    fixed_count = 0
    for plugin_dir in plugin_dirs:
        handler_file = Path(plugin_dir) / 'handler.go'
        if handler_file.exists():
            if fix_file(handler_file):
                fixed_count += 1
        
        gateway_file = Path(plugin_dir) / 'gateway.go'
        if gateway_file.exists():
            if fix_file(gateway_file):
                fixed_count += 1
    
    print(f"\nFixed {fixed_count} files")
    return 0

if __name__ == '__main__':
    sys.exit(main())
