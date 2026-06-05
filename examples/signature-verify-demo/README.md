# signature-verify-demo

Standalone Go program that demonstrates EIP-191 personal-sign signature
verification. The same primitive StreamGate uses for wallet sign-in.

## What it shows

1. EIP-191 prefix wrapping: `\x19Ethereum Signed Message:\n<len><msg>`.
2. ECDSA signing with `crypto.Sign` (simulating MetaMask `personal_sign`).
3. Address recovery from a signature via `crypto.SigToPub`.
4. Tampered message detection (signs `nonce: abc123`, recovers with
   `nonce: HACKED` → addresses do not match).

## Run

```bash
cd examples/signature-verify-demo
go run main.go
```

## Required edits

None — the demo generates a fresh ephemeral key on every run.

## Production path

The StreamGate sign-in flow in `pkg/service/auth_wallet.go` adds three
production concerns on top of this demo:

| Concern | Implementation |
|---------|----------------|
| Replay protection | `nonce` is single-use, stored in Redis with TTL |
| Expiry | `timestamp` in the message is enforced server-side |
| Multi-chain | EIP-191 (EVM) **and** ed25519 (Solana) verifier paths |

## Frontend snippet

The comment block at the bottom of `main.go` shows the JavaScript side:

```js
const sig = await ethereum.request({
  method: 'personal_sign',
  params: [message, userAddress],
});
```

## Output

```
=== 以太坊签名验证示例 ===

📝 原始消息: Sign this message to login: ...

🔑 预期地址: 0x...
🔓 恢复地址: 0x...

✅ 签名验证成功！用户身份确认
   可以发放 JWT token 了

=== 演示：篡改消息 ===
✅ 检测到篡改，验证失败（符合预期）
```
