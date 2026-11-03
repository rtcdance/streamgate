// One process/container per observation, with credentials confined to private
// files. A failed client is retained as a failure; it is never auto-restarted.
import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import { lstat, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createHash } from 'node:crypto';

const methods = new Set(['challenge', 'admit', 'refresh', 'verify', 'manifest', 'segment']);

export class PrivateFileRPC {
    constructor(command, directory) {
        this.command = command;
        this.directory = directory;
        this.sequence = 0;
        this.events = [];
        this.tail = Promise.resolve();
        this.failure = null;
        this.waiter = null;
        this.stderr = createHash('sha256');
    }

    async start() {
        const info = await lstat(this.directory);
        if (!info.isDirectory() || info.isSymbolicLink() || (info.mode & 0o077) || this.child ||
            !Array.isArray(this.command) || !this.command.length || this.command.some(value => typeof value !== 'string')) {
            throw new Error('A fresh file client and private request directory are required');
        }
        this.child = spawn(this.command[0], this.command.slice(1), { stdio: ['pipe', 'pipe', 'pipe'] });
        this.terminal = new Promise(resolve => {
            this.child.once('error', () => { this.fail('File RPC process could not start'); });
            this.child.once('close', (code, signal) => {
                this.exit = { code, signal, stderrSHA256: this.stderr.digest('hex') };
                if (!this.ending || code !== 0 || signal) this.fail('File RPC process terminated before successful EOF');
                resolve(this.exit);
            });
        });
        this.child.stdin.on('error', () => this.fail('File RPC control input closed'));
        this.child.stderr.on('data', bytes => this.stderr.update(bytes));
        this.lines = createInterface({ input: this.child.stdout });
        this.lines.on('line', line => {
            if (!this.waiter) { this.fail('Unexpected file RPC control output'); return; }
            const { accept, resolve, reject, timer } = this.waiter;
            this.waiter = null; clearTimeout(timer);
            if (!accept(line)) { this.fail('Malformed file RPC control output'); reject(this.failure); }
            else resolve(line);
        });
        await this.expect(line => line === 'RPC_READY', 20000);
        return this;
    }

    fail(reason) {
        this.failure ||= new Error(reason);
        if (this.waiter) {
            clearTimeout(this.waiter.timer);
            this.waiter.reject(this.failure); this.waiter = null;
        }
    }

    expect(accept, timeout) {
        if (this.failure) return Promise.reject(this.failure);
        return new Promise((resolve, reject) => {
            const timer = setTimeout(() => this.fail('File RPC exceeded its bounded response time'), timeout);
            this.waiter = { accept, resolve, reject, timer };
        });
    }

    request(method, args = {}) {
        const operation = this.tail.then(() => this.perform(method, args));
        // Serial file commands preserve the original response/status ordering.
        this.tail = operation.catch(() => {});
        return operation;
    }

    async perform(method, args) {
        if (!methods.has(method) || !this.child || this.ending || this.failure || this.exit) throw new Error('File RPC is unavailable or operation is unsupported');
        const sequence = ++this.sequence;
        const input = `${sequence}.request.json`, output = `${sequence}.response.json`;
        const raw = JSON.stringify({ ...args, method });
        if (Buffer.byteLength(raw) > 20 * 1024) throw new Error('File RPC request exceeds its private-file budget');
        await writeFile(join(this.directory, input), raw, { flag: 'wx', mode: 0o600 });
        const completed = this.expect(line => line.startsWith(`RPC_OBSERVED method=${method} code=`), 15000);
        this.child.stdin.write(JSON.stringify({ input, output }) + '\n');
        const line = await completed;
        const path = join(this.directory, output), info = await lstat(path);
        if (!info.isFile() || info.isSymbolicLink() || info.mode & 0o077 || info.size > 2 * 1024 * 1024) throw new Error('File RPC response is missing, nonprivate or oversized');
        const bytes = await readFile(path), value = JSON.parse(bytes);
        if (value.method !== method || typeof value.code !== 'string' || line !== `RPC_OBSERVED method=${method} code=${value.code}`) throw new Error('File RPC original response differs from control output');
        const at = Date.now();
        this.events.push({ sequence, method, code: value.code, at });
        return { value, bytes, at };
    }

    async close() {
        if (!this.child) return null;
        await this.tail;
        this.ending = true;
        this.child.stdin.end();
        const timer = setTimeout(() => this.child.kill('SIGTERM'), 15000);
        try { return await this.terminal; }
        finally { clearTimeout(timer); this.lines.close(); }
    }
}
