# Web3 Development Environment Setup Guide

## 1. Install MetaMask

### 1.1 Browser Extension
- Chrome: https://metamask.io/download/
- After installation, create wallet (save seed phrase!)

### 1.2 Switch to Testnet
1. Click MetaMask network dropdown at top
2. Enable "Show test networks"
3. Select Sepolia testnet

### 1.3 Get Test Coins
- Sepolia Faucet: https://sepoliafaucet.com/
- Requires Alchemy account (free)
- Can claim 0.5 ETH test coins per day

## 2. Get RPC Nodes

### 2.1 Infura (Recommended for Beginners)
1. Register: https://infura.io/
2. Create project
3. Get API Key
4. RPC URL: `https://sepolia.infura.io/v3/YOUR_API_KEY`

### 2.2 Alchemy (More Features)
1. Register: https://www.alchemy.com/
2. Create App
3. Get API Key
4. RPC URL: `https://eth-sepolia.g.alchemy.com/v2/YOUR_API_KEY`

### 2.3 Free Tier
- Infura: 100,000 requests/day
- Alchemy: 300M compute units/month
- Completely sufficient for development!

## 3. Deploy Test NFT Contract

### 3.1 Using Remix (Simplest)
1. Open Remix: https://remix.ethereum.org/
2. Create new file `TestNFT.sol`
3. Paste the following code:

```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/token/ERC721/ERC721.sol";

contract TestNFT is ERC721 {
    uint256 private _tokenIdCounter;
    
    constructor() ERC721("TestNFT", "TNFT") {}
    
    function mint(address to) public {
        _tokenIdCounter++;
        _safeMint(to, _tokenIdCounter);
    }
}
```

4. Compile contract (Ctrl+S)
5. Connect MetaMask
6. Deploy to Sepolia
7. Record contract address!

### 3.2 Mint Test NFT
1. Find deployed contract in Remix
2. Call `mint` function
3. Enter your wallet address
4. Confirm transaction
5. Wait for confirmation (~15 seconds)

### 3.3 Verify NFT
- View on Sepolia Etherscan: https://sepolia.etherscan.io/
- Search your wallet address
- Should see your NFT

## 4. Solana Development Environment

### 4.1 Install Solana CLI
```bash
sh -c "$(curl -sSfL https://release.solana.com/stable/install)"
```

### 4.2 Configure Testnet
```bash
solana config set --url https://api.devnet.solana.com
```

### 4.3 Create Wallet
```bash
solana-keygen new
```

### 4.4 Get Test Coins
```bash
solana airdrop 2
```

### 4.5 Phantom Wallet
- Install: https://phantom.app/
- Switch to Devnet
- Use for testing Solana NFT

## 5. Development Tools

### 5.1 Blockchain Explorers
- Ethereum Sepolia: https://sepolia.etherscan.io/
- Polygon Mumbai: https://mumbai.polygonscan.com/
- Solana Devnet: https://explorer.solana.com/?cluster=devnet

### 5.2 Go Dependencies
```bash
go get github.com/ethereum/go-ethereum
go get github.com/gagliardetto/solana-go
```

### 5.3 Testing Tools
- Postman: Test APIs
- k6: Performance testing
- curl: Quick testing

## 6. Common Questions

### Q: RPC request fails?
A: 
1. Check if API Key is correct
2. Check if network is correct (mainnet vs testnet)
3. Check if free tier limit exceeded
4. Try switching to another RPC provider

### Q: Transaction always pending?
A: 
1. Gas fee might be set too low
2. Network congestion
3. Wait longer (testnet can be slow)

### Q: Contract call returns error?
A: 
1. Check if contract address is correct
2. Check if ABI matches
3. Check function parameter types
4. View transaction details on Etherscan

## 7. Security Notes

### ⚠️ Never:
- Share private key or seed phrase
- Hardcode private key in code
- Commit private key to Git
- Use testnet private key on mainnet

### ✅ Should Do:
- Use environment variables for sensitive info
- Use .gitignore to ignore config files
- Use different wallets for testnet and mainnet
- Backup seed phrase offline regularly

## 8. Learning Resources

### Official Documentation
- Ethereum: https://ethereum.org/developers
- Solana: https://docs.solana.com/
- OpenZeppelin: https://docs.openzeppelin.com/

### Tutorials
- CryptoZombies: https://cryptozombies.io/ (Solidity intro)
- Solana Cookbook: https://solanacookbook.com/
- Ethereum Book: https://github.com/ethereumbook/ethereumbook

### Community
- Ethereum Stack Exchange
- Solana Discord
- Web3 Developer Community

## 9. Quick Verification Checklist

Before starting project, ensure you can:
- [ ] Connect to testnet RPC
- [ ] Query wallet balance
- [ ] Query NFT balance
- [ ] Verify signature
- [ ] Deploy simple contract
- [ ] Call contract function

If you can do all above, you're ready to start the project!
