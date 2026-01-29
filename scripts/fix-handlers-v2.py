#!/usr/bin/env python3
"""Fix handler files by removing security/optimization refs."""

import re

def fix_file(filepath):
    """Fix a single file."""
    print(f"Fixing {filepath}...")
    
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # Remove imports
    content = re.sub(r'\n\s+"streamgate/pkg/security"', '', content)
    content = re.sub(r'\n\s+"streamgate/pkg/optimization"', '', content)
    
    # Remove struct fields
    content = re.sub(r'\n\s+rateLimiter\s+\*security\.RateLimiter', '', content)
    content = re.sub(r'\n\s+auditLogger\s+\*security\.AuditLogger', '', content)
    content = re.sub(r'\n\s+cache\s+\*optimization\.LocalCache', '', content)
    content = re.sub(r'\n\s+localCache\s+\*optimization\.LocalCache', '', content)
    
    # Remove initialization lines
    content = re.sub(r'\n\s+rateLimiter:\s+security\.NewRateLimiter\([^)]+\),?', '', content)
    content = re.sub(r'\n\s+auditLogger:\s+security\.NewAuditLogger\([^)]+\),?', '', content)
    content = re.sub(r'\n\s+cache:\s+optimization\.NewLocalCache\([^)]+\),?', '', content)
    content = re.sub(r'\n\s+localCache:\s+optimization\.NewLocalCache\([^)]+\),?', '', content)
    
    # Remove unused variables
    content = re.sub(r'\n\s+startTime := time\.Now\(\)', '', content)
    content = re.sub(r'\n\s+clientIP := r\.RemoteAddr', '', content)
    
    # Remove audit logger calls
    content = re.sub(r'\n\s+[hp]\.auditLogger\.LogEvent\([^)]+\)', '', content)
    
    # Remove RecordTimer with time.Since
    content = re.sub(r'\n\s+[hp]\.metricsCollector\.RecordTimer\([^,]+, time\.Since\(startTime\)[^)]+\)', '', content)
    
    # Remove cache operations
    content = re.sub(r'\n\s+[hp]\.cache\.Stop\(\)', '', content)
    content = re.sub(r'\n\s+[hp]\.cache\.Set\([^)]+\)', '', content)
    
    # Remove rate limit blocks (multiline)
    lines = content.split('\n')
    new_lines = []
    skip = False
    brace_count = 0
    
    for line in lines:
        if 'if !h.rateLimiter.Allow(' in line or 'if !p.rateLimiter.Allow(' in line:
            skip = True
            brace_count = line.count('{') - line.count('}')
            continue
        if skip:
            brace_count += line.count('{') - line.count('}')
            if brace_count <= 0:
                skip = False
            continue
        # Remove cache check blocks
        if 'if cached, ok := h.cache.Get(' in line:
            skip = True
            brace_count = line.count('{') - line.count('}')
            continue
        new_lines.append(line)
    
    content = '\n'.join(new_lines)
    
    # Remove cache stop blocks
    content = re.sub(r'\n\s+if p\.cache != nil \{\n\s+p\.cache\.Stop\(\)\n\s+\}', '', content)
    
    # Remove time import if not used elsewhere
    if 'time.Duration' not in content and 'time.Time' not in content and 'time.Second' not in content:
        content = re.sub(r'\n\s+"time"', '', content)
    
    if content != original:
        with open(filepath, 'w') as f:
            f.write(content)
        print(f"  ✓ Fixed")
        return True
    else:
        print(f"  - No changes")
        return False

if __name__ == '__main__':
    files = [
        'pkg/plugins/api/handler.go',
        'pkg/plugins/cache/handler.go',
        'pkg/plugins/metadata/handler.go',
        'pkg/plugins/transcoder/handler.go',
        'pkg/plugins/upload/handler.go',
    ]
    
    for f in files:
        fix_file(f)
