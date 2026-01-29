#!/usr/bin/env python3
"""
Fix plugin files by removing security/optimization package references
and fixing zap logger syntax errors.
"""

import re
import sys

def fix_plugin_file(filepath):
    """Fix a single plugin file."""
    print(f"Fixing {filepath}...")
    
    with open(filepath, 'r') as f:
        content = f.read()
    
    original_content = content
    
    # Remove security and optimization imports
    content = re.sub(r'\s*"streamgate/pkg/security"\n', '', content)
    content = re.sub(r'\s*"streamgate/pkg/optimization"\n', '', content)
    
    # Remove struct fields
    content = re.sub(r'\s*rateLimiter\s+\*security\.RateLimiter\n', '', content)
    content = re.sub(r'\s*auditLogger\s+\*security\.AuditLogger\n', '', content)
    content = re.sub(r'\s*cache\s+\*optimization\.LocalCache\n', '', content)
    content = re.sub(r'\s*localCache\s+\*optimization\.LocalCache\n', '', content)
    
    # Remove initialization lines in New* functions
    content = re.sub(r'\s*rateLimiter:\s+security\.NewRateLimiter\([^)]+\),?\n', '', content)
    content = re.sub(r'\s*auditLogger:\s+security\.NewAuditLogger\([^)]+\),?\n', '', content)
    content = re.sub(r'\s*cache:\s+optimization\.NewLocalCache\([^)]+\),?\n', '', content)
    content = re.sub(r'\s*localCache:\s+optimization\.NewLocalCache\([^)]+\),?\n', '', content)
    
    # Remove standalone initialization statements
    content = re.sub(r'\s*[hp]\.(rateLimiter|auditLogger|cache|localCache)\s*=\s*[^;]+\n', '', content)
    
    # Remove audit logger calls - handle multiline
    content = re.sub(r'\s*[hp]\.auditLogger\.Log[^;]+\n', '', content)
    content = re.sub(r'\s*[hp]\.auditLogger\.LogEvent\([^)]+\)\n', '', content)
    # Handle multiline audit logger calls
    lines = content.split('\n')
    new_lines = []
    skip_until_paren = 0
    for line in lines:
        if skip_until_paren > 0:
            skip_until_paren += line.count('(') - line.count(')')
            if skip_until_paren <= 0:
                skip_until_paren = 0
            continue
        if re.search(r'[hp]\.auditLogger\.LogEvent\(', line):
            skip_until_paren = 1 + line.count('(') - line.count(')')
            if skip_until_paren <= 0:
                skip_until_paren = 0
            continue
        new_lines.append(line)
    content = '\n'.join(new_lines)
    
    # Remove cache operations
    content = re.sub(r'\s*if\s+p\.cache\s*!=\s*nil\s*\{[^}]+\}\n', '', content, flags=re.DOTALL)
    content = re.sub(r'\s*[hp]\.cache\.Stop\(\)\n', '', content)
    content = re.sub(r'\s*[hp]\.cache\.Set\([^)]+\)\n', '', content)
    
    # Remove cache check blocks (multiline)
    lines = content.split('\n')
    new_lines = []
    skip_cache_block = False
    brace_count = 0
    for line in lines:
        if 'if cached, ok := h.cache.Get(' in line:
            skip_cache_block = True
            brace_count = line.count('{') - line.count('}')
            continue
        if skip_cache_block:
            brace_count += line.count('{') - line.count('}')
            if brace_count <= 0:
                skip_cache_block = False
            continue
        new_lines.append(line)
    content = '\n'.join(new_lines)
    
    # Remove rate limit check blocks (multiline) - be careful to only remove the if block
    lines = content.split('\n')
    new_lines = []
    skip_rate_limit = False
    brace_count = 0
    for line in lines:
        if 'if !h.rateLimiter.Allow(' in line or 'if !p.rateLimiter.Allow(' in line:
            skip_rate_limit = True
            brace_count = line.count('{') - line.count('}')
            continue
        if skip_rate_limit:
            brace_count += line.count('{') - line.count('}')
            if brace_count <= 0:
                skip_rate_limit = False
            continue
        new_lines.append(line)
    content = '\n'.join(new_lines)
    
    # Remove unused variables after rate limiter removal
    content = re.sub(r'\s*clientIP\s*:=\s*r\.RemoteAddr\n', '', content)
    content = re.sub(r'\s*startTime\s*:=\s*time\.Now\(\)\n', '', content)
    
    # Fix zap logger calls - wrap bare strings with zap.String()
    # Pattern: logger.Method("message", "key", value, ...) -> logger.Method("message", zap.String("key", value), ...)
    # This is complex, so we'll handle specific cases
    
    # Fix: "key", value -> zap.String("key", value) for string values
    # Fix: "key", id -> zap.String("key", id) for ID fields
    content = re.sub(
        r'h\.logger\.(Info|Error|Warn|Debug)\(([^,]+),\s*"([^"]+)",\s*([^,\)]+)',
        lambda m: f'h.logger.{m.group(1)}({m.group(2)}, zap.String("{m.group(3)}", {m.group(4)})',
        content
    )
    content = re.sub(
        r'p\.logger\.(Info|Error|Warn|Debug)\(([^,]+),\s*"([^"]+)",\s*([^,\)]+)',
        lambda m: f'p.logger.{m.group(1)}({m.group(2)}, zap.String("{m.group(3)}", {m.group(4)})',
        content
    )
    
    if content != original_content:
        with open(filepath, 'w') as f:
            f.write(content)
        print(f"  ✓ Fixed {filepath}")
        return True
    else:
        print(f"  - No changes needed for {filepath}")
        return False

if __name__ == '__main__':
    files = [
        'pkg/plugins/api/gateway.go',
        'pkg/plugins/api/handler.go',
        'pkg/plugins/auth/handler.go',
        'pkg/plugins/cache/handler.go',
        'pkg/plugins/metadata/handler.go',
        'pkg/plugins/monitor/handler.go',
        'pkg/plugins/streaming/handler.go',
        'pkg/plugins/transcoder/handler.go',
        'pkg/plugins/upload/handler.go',
        'pkg/plugins/worker/handler.go',
    ]
    
    fixed_count = 0
    for filepath in files:
        try:
            if fix_plugin_file(filepath):
                fixed_count += 1
        except Exception as e:
            print(f"  ✗ Error fixing {filepath}: {e}")
            sys.exit(1)
    
    print(f"\n✓ Fixed {fixed_count} files")
