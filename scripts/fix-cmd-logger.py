#!/usr/bin/env python3
"""Fix logger errors in cmd files"""

import re
import sys
from pathlib import Path

def fix_cmd_file(filepath):
    """Fix logger errors in a cmd main.go file"""
    with open(filepath, 'r') as f:
        content = f.read()
    
    # Add zap import if not present
    if '"go.uber.org/zap"' not in content:
        content = re.sub(
            r'(import \(\n)',
            r'\1\t"go.uber.org/zap"\n',
            content
        )
    
    # Fix log.Fatal calls
    content = re.sub(
        r'log\.Fatal\("([^"]+)", "error", err\)',
        r'log.Fatal("\1", zap.Error(err))',
        content
    )
    
    # Fix log.Info with mode, service, port
    content = re.sub(
        r'log\.Info\("Configuration loaded", "mode", cfg\.Mode, "service", cfg\.ServiceName, "port", cfg\.Server\.Port\)',
        r'log.Info("Configuration loaded",\n\t\tzap.String("mode", cfg.Mode),\n\t\tzap.String("service", cfg.ServiceName),\n\t\tzap.Int("port", cfg.Server.Port))',
        content
    )
    
    # Fix log.Info with port
    content = re.sub(
        r'log\.Info\("([^"]+) started successfully", "port", cfg\.Server\.Port\)',
        r'log.Info("\1 started successfully", zap.Int("port", cfg.Server.Port))',
        content
    )
    
    # Fix log.Info with signal
    content = re.sub(
        r'log\.Info\("Received shutdown signal", "signal", sig\)',
        r'log.Info("Received shutdown signal", zap.String("signal", sig.String()))',
        content
    )
    
    # Fix log.Error with error
    content = re.sub(
        r'log\.Error\("Error during shutdown", "error", err\)',
        r'log.Error("Error during shutdown", zap.Error(err))',
        content
    )
    
    with open(filepath, 'w') as f:
        f.write(content)
    
    print(f"Fixed: {filepath}")

def main():
    # Fix all cmd files
    cmd_files = [
        "cmd/microservices/cache/main.go",
        "cmd/microservices/metadata/main.go",
        "cmd/microservices/monitor/main.go",
        "cmd/microservices/streaming/main.go",
        "cmd/microservices/transcoder/main.go",
        "cmd/microservices/upload/main.go",
        "cmd/microservices/worker/main.go",
    ]
    
    for filepath in cmd_files:
        if Path(filepath).exists():
            fix_cmd_file(filepath)
        else:
            print(f"Not found: {filepath}")

if __name__ == "__main__":
    main()
