#!/usr/bin/env python3
"""
Comprehensive zap logger fixer
Converts all incorrect zap logger calls to use proper field constructors
"""

import re
import os
import sys
from pathlib import Path

# Map of field names to their likely types
FIELD_TYPES = {
    # String fields
    'address': 'String',
    'chain_id': 'String',
    'contract': 'String',
    'cid': 'String',
    'filename': 'String',
    'name': 'String',
    'service_id': 'String',
    'service_name': 'String',
    'path': 'String',
    'method': 'String',
    'from': 'String',
    'to': 'String',
    'key': 'String',
    'type': 'String',
    'subject': 'String',
    'event_type': 'String',
    'event_id': 'String',
    'tx_hash': 'String',
    'function': 'String',
    'mode': 'String',
    'url': 'String',
    'version': 'String',
    'rpc_url': 'String',
    'host': 'String',
    'challenge_id': 'String',
    'from_chain': 'String',
    'alert_id': 'String',
    'signal': 'String',
    'owner': 'String',
    'token_id': 'String',
    'asset': 'String',
    'amount': 'String',
    'event': 'String',
    'from_block': 'String',
    'to_block': 'String',
    'value': 'String',
    'gas_limit': 'String',
    
    # Int fields
    'port': 'Int',
    'count': 'Int',
    'size': 'Int',
    'nonce': 'Int',
    'failures': 'Int',
    
    # Int64 fields
    'id': 'Int64',
    'user_id': 'Int64',
    'content_id': 'Int64',
    'block_number': 'Int64',
    
    # Duration fields
    'duration': 'Duration',
    'elapsed': 'Duration',
    'update_interval': 'Duration',
    
    # Boolean fields
    'success': 'Bool',
    'is_owner': 'Bool',
    'has_nft': 'Bool',
    'is_contract': 'Bool',
    
    # Special fields
    'error': 'Error',
    'message_length': 'Int',
    'length': 'Int',
    'expected': 'String',
    'gas_price_wei': 'String',
    'gas_price': 'String',
    'gas': 'String',
    'balance': 'String',
}

def fix_logger_call(match):
    """Fix a single logger call"""
    logger_call = match.group(1)  # logger.Error, logger.Info, etc.
    message = match.group(2)
    rest = match.group(3)
    
    # Parse key-value pairs
    pairs = []
    current_pos = 0
    
    while current_pos < len(rest):
        # Skip whitespace and commas
        while current_pos < len(rest) and rest[current_pos] in ', \t\n':
            current_pos += 1
        
        if current_pos >= len(rest):
            break
        
        # Check if this is "error", err pattern
        if rest[current_pos:].startswith('"error"'):
            # Find the error variable
            error_match = re.match(r'"error",\s*(\w+)', rest[current_pos:])
            if error_match:
                pairs.append(f'zap.Error({error_match.group(1)})')
                current_pos += error_match.end()
                continue
        
        # Try to match a key-value pair
        kv_match = re.match(r'"([^"]+)",\s*([^,)]+)', rest[current_pos:])
        if kv_match:
            key = kv_match.group(1)
            value = kv_match.group(2).strip()
            
            # Determine the zap field type
            field_type = FIELD_TYPES.get(key, 'String')
            
            # Handle special cases
            if value in ['err', 'error'] and key == 'error':
                pairs.append(f'zap.Error({value})')
            elif field_type == 'String' and not value.startswith('"'):
                pairs.append(f'zap.String("{key}", {value})')
            elif field_type == 'Int':
                pairs.append(f'zap.Int("{key}", {value})')
            elif field_type == 'Int64':
                pairs.append(f'zap.Int64("{key}", {value})')
            elif field_type == 'Duration':
                pairs.append(f'zap.Duration("{key}", {value})')
            elif field_type == 'Bool':
                pairs.append(f'zap.Bool("{key}", {value})')
            else:
                pairs.append(f'zap.String("{key}", {value})')
            
            current_pos += kv_match.end()
        else:
            # Can't parse, move forward
            current_pos += 1
    
    # Reconstruct the call
    if pairs:
        return f'{logger_call}({message}, {", ".join(pairs)})'
    else:
        return f'{logger_call}({message})'

def fix_file(filepath):
    """Fix all logger calls in a file"""
    try:
        with open(filepath, 'r', encoding='utf-8') as f:
            content = f.read()
        
        original_content = content
        
        # Pattern to match logger calls with key-value pairs
        # Matches: logger.Error("msg", "key", value, "key2", value2, ...)
        pattern = r'(logger\.(Error|Warn|Info|Debug))\(([^,]+),\s*("(?:[^"]|\\")*",\s*[^)]+)\)'
        
        content = re.sub(pattern, fix_logger_call, content)
        
        # Check if file was modified
        if content != original_content:
            with open(filepath, 'w', encoding='utf-8') as f:
                f.write(content)
            return True
        return False
    except Exception as e:
        print(f"Error processing {filepath}: {e}", file=sys.stderr)
        return False

def main():
    """Main function"""
    base_dir = Path('.')
    
    # Find all Go files
    go_files = list(base_dir.glob('pkg/**/*.go')) + list(base_dir.glob('cmd/**/*.go'))
    
    modified_count = 0
    for filepath in go_files:
        if fix_file(filepath):
            print(f"✓ Modified: {filepath}")
            modified_count += 1
        else:
            print(f"  No changes: {filepath}")
    
    print(f"\n✓ Fixed {modified_count} files")
    print("\nNext: Run goimports to fix imports")

if __name__ == '__main__':
    main()
