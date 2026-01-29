#!/usr/bin/env python3
"""
Fix all remaining zap logger errors in the codebase.
This script converts incorrect logger calls to proper zap.Field format.
"""

import re
import os
import sys

def fix_logger_call(line):
    """Fix a single logger call line."""
    # Pattern: logger.Method("msg", "key1", val1, "key2", val2, ...)
    # Should be: logger.Method("msg", zap.Type("key1", val1), zap.Type("key2", val2), ...)
    
    # Match logger calls with multiple arguments
    pattern = r'(logger\.(Info|Debug|Warn|Error|Fatal))\("([^"]+)"((?:,\s*"[^"]+",\s*[^,)]+)+)\)'
    
    def replace_args(match):
        method = match.group(1)
        msg = match.group(3)
        args_str = match.group(4)
        
        # Split arguments into key-value pairs
        # This regex finds: , "key", value
        arg_pattern = r',\s*"([^"]+)",\s*([^,)]+)'
        args = re.findall(arg_pattern, args_str)
        
        if not args:
            return match.group(0)
        
        # Convert each key-value pair to zap.Field
        zap_fields = []
        for key, value in args:
            # Determine the zap field type based on the value
            value = value.strip()
            
            if value == 'err' or 'error' in key.lower():
                zap_fields.append(f'zap.Error({value})')
            elif value.startswith('"') and value.endswith('"'):
                zap_fields.append(f'zap.String("{key}", {value})')
            elif value.isdigit() or value.startswith('-') and value[1:].isdigit():
                zap_fields.append(f'zap.Int("{key}", {value})')
            elif value in ['true', 'false']:
                zap_fields.append(f'zap.Bool("{key}", {value})')
            elif '.Int64()' in value or 'int64' in value.lower():
                zap_fields.append(f'zap.Int64("{key}", {value})')
            elif 'time.' in value or 'duration' in key.lower():
                zap_fields.append(f'zap.Duration("{key}", {value})')
            else:
                # Default to String for unknown types
                zap_fields.append(f'zap.String("{key}", {value})')
        
        # Reconstruct the logger call
        result = f'{method}("{msg}",\n\t\t'
        result += ',\n\t\t'.join(zap_fields)
        result += ')'
        
        return result
    
    return re.sub(pattern, replace_args, line)

def process_file(filepath):
    """Process a single Go file."""
    try:
        with open(filepath, 'r', encoding='utf-8') as f:
            content = f.read()
        
        original = content
        
        # Fix logger calls
        lines = content.split('\n')
        fixed_lines = []
        for line in lines:
            if 'logger.' in line and ('Info(' in line or 'Debug(' in line or 'Warn(' in line or 'Error(' in line or 'Fatal(' in line):
                fixed_line = fix_logger_call(line)
                fixed_lines.append(fixed_line)
            else:
                fixed_lines.append(line)
        
        content = '\n'.join(fixed_lines)
        
        if content != original:
            with open(filepath, 'w', encoding='utf-8') as f:
                f.write(content)
            print(f"Fixed: {filepath}")
            return True
        
        return False
    except Exception as e:
        print(f"Error processing {filepath}: {e}", file=sys.stderr)
        return False

def main():
    """Main function."""
    # Directories to process
    dirs = [
        'pkg/web3',
        'pkg/service',
        'cmd/microservices',
        'cmd/monolith'
    ]
    
    fixed_count = 0
    for dir_path in dirs:
        if not os.path.exists(dir_path):
            continue
        
        for root, _, files in os.walk(dir_path):
            for file in files:
                if file.endswith('.go'):
                    filepath = os.path.join(root, file)
                    if process_file(filepath):
                        fixed_count += 1
    
    print(f"\nTotal files fixed: {fixed_count}")

if __name__ == '__main__':
    main()
