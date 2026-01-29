#!/usr/bin/env python3
"""Fix all plugin handler files by removing security/optimization refs and fixing zap logger calls."""

import re
import sys

def remove_rate_limit_blocks(content):
    """Remove rate limiter check blocks carefully."""
    lines = content.split('\n')
    new_lines = []
    skip_rate_limit = False
    brace_count = 0
    
    for line in lines:
        if ('if !h.rateLimiter.Allow(' in line or 'if !p.rateLimiter.Allow(' in line):
            skip_rate_limit = True
            brace_count = line.count('{') - line.count('}')
            continue
        if skip_rate_limit:
            brace_count += line.count('{') - line.count('}')
            if brace_count <= 0:
                skip_rate_limit = False
            continue
        new_lines.append(line)
    
    return '\n'.join(new_lines)

def remove_cache_blocks(content):
    """Remove cache check blocks."""
    lines = content.split('\n')
    new_lines = []
    skip_cache = False
    brace_count = 0
    
    for line in lines:
        if 'if cached, ok := h.cache.Get(' in line:
            skip_cache = True
            brace_count = line.count('{') - line.count('}')
            continue
        if skip_cache:
            brace_count += line.count('{') - line.count('}')
            if brace_count <= 0:
                skip_cache = False
            continue
        new_lines.append(line)
    
    return '\n'.join(new_lines)

def fix_handler_file(filepath):
    """Fix a handler file."""
    print(f"Fixing {filepath}...")
    
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # Remove imports
    content = re.sub(r'\s+"streamgate/pkg/security"\n', '', content)
    content = re.sub(r'\s+"streamgate/pkg/optimization"\n', '', content)
    content = re.sub(r'\s+"time"\n', '', content)  # Remove time if not needed
    
    # Remove struct fields
    content = re.sub(r'\s+rateLimiter\s+\*security\.RateLimiter\n', '', content)
    content = re.sub(r'\s+auditLogger\s+\*security\.AuditLogger\n', '', content)
    content = re.sub(r'\s+cache\s+\*optimization\.LocalCache\n', '', content)
    content = re.sub(r'\s+localCache\s+\*optimization\.LocalCache\n', '', content)
    
    # Remove initialization in New* functions
    content = re.sub(r'\s+rateLimiter:\s+security\.NewRateLimiter\([^)]+\),?\n', '', content)
    content = re.sub(r'\s+auditLogger:\s+security\.NewAuditLogger\([^)]+\),?\n', '', content)
    content = re.sub(r'\s+cache:\s+optimization\.NewLocalCache\([^)]+\),?\n', '', content)
    content = re.sub(r'\s+localCache:\s+optimization\.NewLocalCache\([^)]+\),?\n', '', content)
    
    # Remove unused variables
    content = re.sub(r'\s+startTime := time\.Now\(\)\n', '', content)
    content = re.sub(r'\s+clientIP := r\.RemoteAddr\n', '', content)
    
    # Remove audit logger calls (single line)
    content = re.sub(r'\s+[hp]\.auditLogger\.LogEvent\([^\n]+\n', '', content)
    
    # Remove RecordTimer calls with time.Since
    content = re.sub(r'\s+[hp]\.metricsCollector\.RecordTimer\([^,]+, time\.Since\(startTime\)[^\n]+\n', '', content)
    
    # Remove cache operations
    content = re.sub(r'\s+[hp]\.cache\.Stop\(\)\n', '', content)
    content = re.sub(r'\s+[hp]\.cache\.Set\([^\n]+\n', '', content)
    
    # Remove cache stop blocks
    content = re.sub(r'\s+if p\.cache != nil \{\s+p\.cache\.Stop\(\)\s+\}\n', '', content, flags=re.DOTALL)
    
    # Remove rate limit and cache blocks
    content = remove_rate_limit_blocks(content)
    content = remove_cache_blocks(content)
    
    if content != original:
        with open(filepath, 'w') as f:
            f.write(content)
        print(f"  ✓ Fixed {filepath}")
        return True
    else:
        print(f"  - No changes for {filepath}")
        return False

if __name__ == '__main__':
    files = [
        'pkg/plugins/api/handler.go',
        'pkg/plugins/cache/handler.go',
        'pkg/plugins/metadata/handler.go',
        'pkg/plugins/transcoder/handler.go',
        'pkg/plugins/upload/handler.go',
        'pkg/plugins/worker/handler.go',
    ]
    
    fixed = 0
    for f in files:
        try:
            if fix_handler_file(f):
                fixed += 1
        except Exception as e:
            print(f"  ✗ Error: {e}")
            sys.exit(1)
    
    print(f"\n✓ Fixed {fixed} files")
