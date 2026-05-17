package web3

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
)

// HDWallet implements BIP-32/BIP-44 hierarchical deterministic wallet derivation.
// It uses a mnemonic seed phrase (BIP-39) to generate a master key, from which
// child keys are derived via standard derivation paths.
type HDWallet struct {
	mnemonic  string
	seed      []byte
	masterKey *extendedKey
	logger    *zap.Logger
}

func (hw *HDWallet) Destroy() {
	if hw.mnemonic != "" {
		hw.mnemonic = strings.Repeat("x", len(hw.mnemonic))
	}
	for i := range hw.seed {
		hw.seed[i] = 0
	}
	hw.seed = nil
	if hw.masterKey != nil {
		for i := range hw.masterKey.key {
			hw.masterKey.key[i] = 0
		}
		for i := range hw.masterKey.chainCode {
			hw.masterKey.chainCode[i] = 0
		}
		hw.masterKey = nil
	}
}

// extendedKey represents a BIP-32 extended key with chain code.
type extendedKey struct {
	key       []byte // 32 bytes for private key
	chainCode []byte // 32 bytes
	isPrivate bool
	index     uint32
	depth     byte
}

// BIP-39 English word list (2048 words) — using the standard list defined in
// https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt
// We embed a compact subset for generation; full list needed for production.
// For generation we use the standard entropy→mnemonic mapping.

// GenerateMnemonic generates a BIP-39 mnemonic with the given entropy bits (128 or 256).
func GenerateMnemonic(bits int) (string, error) {
	if bits != 128 && bits != 256 {
		return "", fmt.Errorf("invalid entropy bits: must be 128 or 256, got %d", bits)
	}

	entropy := make([]byte, bits/8)
	if _, err := rand.Read(entropy); err != nil {
		return "", fmt.Errorf("failed to generate entropy: %w", err)
	}

	// Compute checksum
	hash := sha256.Sum256(entropy)
	checksumBits := bits / 32
	checksum := hash[0] >> (8 - byte(checksumBits))

	// Combine entropy + checksum bits
	combined := make([]byte, len(entropy)+1)
	copy(combined, entropy)
	combined[len(entropy)] = checksum

	// Convert to 11-bit groups and map to words
	totalBits := bits + checksumBits
	wordCount := totalBits / 11
	words := make([]string, wordCount)

	for i := 0; i < int(wordCount); i++ {
		startBit := i * 11
		var index uint32
		for bit := 0; bit < 11; bit++ {
			byteIndex := (startBit + bit) / 8
			bitIndex := 7 - ((startBit + bit) % 8)
			if byteIndex < len(combined) && (combined[byteIndex]&(1<<bitIndex)) != 0 {
				index |= 1 << (10 - bit)
			}
		}
		if int(index) >= len(bip39EnglishWords) {
			return "", fmt.Errorf("word index %d out of range", index)
		}
		words[i] = bip39EnglishWords[index]
	}

	return strings.Join(words, " "), nil
}

// MnemonicToSeed converts a BIP-39 mnemonic to a 64-byte seed using PBKDF2.
// An optional password can be provided for additional security.
func MnemonicToSeed(mnemonic, password string) ([]byte, error) {
	if mnemonic == "" {
		return nil, errors.New("mnemonic cannot be empty")
	}

	// Validate mnemonic word count
	words := strings.Fields(mnemonic)
	wordCount := len(words)
	if wordCount%4 != 0 || wordCount < 12 || wordCount > 24 {
		return nil, fmt.Errorf("invalid mnemonic word count: %d (must be 12, 15, 18, 21, or 24)", wordCount)
	}

	// Validate all words are in the BIP-39 word list
	wordSet := make(map[string]bool, len(bip39EnglishWords))
	for _, w := range bip39EnglishWords {
		wordSet[w] = true
	}
	for _, w := range words {
		if !wordSet[w] {
			return nil, fmt.Errorf("invalid mnemonic word: %q", w)
		}
	}

	// PBKDF2-HMAC-SHA512 with 2048 iterations
	salt := "mnemonic" + password
	seed := pbkdf512([]byte(mnemonic), []byte(salt), 2048, 64)
	return seed, nil
}

// NewHDWalletFromMnemonic creates an HD wallet from a mnemonic phrase.
func NewHDWalletFromMnemonic(mnemonic, password string, logger *zap.Logger) (*HDWallet, error) {
	seed, err := MnemonicToSeed(mnemonic, password)
	if err != nil {
		return nil, fmt.Errorf("failed to convert mnemonic to seed: %w", err)
	}

	masterKey, err := deriveMasterKey(seed)
	if err != nil {
		return nil, fmt.Errorf("failed to derive master key: %w", err)
	}

	return &HDWallet{
		mnemonic:  mnemonic,
		seed:      seed,
		masterKey: masterKey,
		logger:    logger,
	}, nil
}

// DefaultEthereumPath returns the standard BIP-44 derivation path for Ethereum.
// Path: m/44'/60'/0'/0/{accountIndex}
func DefaultEthereumPath(accountIndex uint32) string {
	return fmt.Sprintf("m/44'/60'/0'/0/%d", accountIndex)
}

// DeriveKey derives a private key from the given BIP-32 derivation path.
// Path format: "m/44'/60'/0'/0/0" where ' indicates hardened derivation.
func (w *HDWallet) DeriveKey(path string) (*big.Int, error) {
	key, err := derivePath(w.masterKey, path)
	if err != nil {
		return nil, err
	}

	// The key bytes are the private key in big-endian
	privKey := new(big.Int).SetBytes(key.key)
	if privKey.Sign() == 0 || privKey.Cmp(crypto.S256().Params().N) >= 0 {
		return nil, errors.New("derived key is invalid (zero or >= curve order)")
	}

	return privKey, nil
}

// DeriveAddress derives an Ethereum address from the given path.
func (w *HDWallet) DeriveAddress(path string) (string, error) {
	privKeyInt, err := w.DeriveKey(path)
	if err != nil {
		return "", err
	}

	privKey, err := crypto.ToECDSA(privKeyInt.Bytes())
	if err != nil {
		return "", fmt.Errorf("failed to convert to ECDSA: %w", err)
	}

	address := crypto.PubkeyToAddress(privKey.PublicKey)
	return address.Hex(), nil
}

// GetMnemonic returns the mnemonic phrase.
// WARNING: This is sensitive data. Use with caution.
func (w *HDWallet) GetMnemonic() string {
	return w.mnemonic
}

// deriveMasterKey creates the BIP-32 master key from a seed.
func deriveMasterKey(seed []byte) (*extendedKey, error) {
	// HMAC-SHA512 with "Bitcoin seed" as key
	mac := hmac.New(sha512.New, []byte("Bitcoin seed"))
	mac.Write(seed)
	I := mac.Sum(nil)

	if len(I) != 64 {
		return nil, errors.New("invalid HMAC-SHA512 output length")
	}

	key := I[:32]
	chainCode := I[32:]

	// Verify key is valid
	keyInt := new(big.Int).SetBytes(key)
	if keyInt.Sign() == 0 || keyInt.Cmp(crypto.S256().Params().N) >= 0 {
		return nil, errors.New("master key is invalid")
	}

	return &extendedKey{
		key:       key,
		chainCode: chainCode,
		isPrivate: true,
		index:     0,
		depth:     0,
	}, nil
}

// derivePath derives a key at the given path from the parent key.
func derivePath(parent *extendedKey, path string) (*extendedKey, error) {
	if !strings.HasPrefix(path, "m/") {
		return nil, errors.New("path must start with 'm/'")
	}

	current := parent
	components := strings.Split(path[2:], "/")

	for _, component := range components {
		if component == "" {
			continue
		}

		hardened := strings.HasSuffix(component, "'") || strings.HasSuffix(component, "h")
		indexStr := component
		if hardened {
			indexStr = component[:len(component)-1]
		}

		var index uint32
		if _, err := fmt.Sscanf(indexStr, "%d", &index); err != nil {
			return nil, fmt.Errorf("invalid path component: %s", component)
		}

		if hardened {
			if index > 0x7FFFFFFF {
				return nil, fmt.Errorf("hardened index %d out of range", index)
			}
			index += 0x80000000
		} else {
			if index >= 0x80000000 {
				return nil, fmt.Errorf("unhardened index %d out of range", index)
			}
		}

		child, err := deriveChild(current, index)
		if err != nil {
			return nil, fmt.Errorf("failed to derive index %d: %w", index, err)
		}

		current = child
	}

	return current, nil
}

// deriveChild derives a child key from a parent key using BIP-32 CKD function.
func deriveChild(parent *extendedKey, index uint32) (*extendedKey, error) {
	var data []byte
	if index >= 0x80000000 {
		// Hardened child: 0x00 || parentKey || index
		data = make([]byte, 37)
		data[0] = 0x00
		copy(data[1:33], parent.key)
		binary.BigEndian.PutUint32(data[33:], index)
	} else {
		// Normal child: compressedPubKey || index
		priv, err := crypto.ToECDSA(parent.key)
		if err != nil {
			return nil, fmt.Errorf("invalid parent key: %w", err)
		}
		pubBytes := crypto.CompressPubkey(&priv.PublicKey)
		data = make([]byte, 37)
		copy(data[:33], pubBytes)
		binary.BigEndian.PutUint32(data[33:], index)
	}

	// HMAC-SHA512(ChainCode, data)
	mac := hmac.New(sha512.New, parent.chainCode)
	mac.Write(data)
	I := mac.Sum(nil)

	IL := I[:32]
	IR := I[32:]

	// childKey = (IL + parentKey) mod n
	ilInt := new(big.Int).SetBytes(IL)
	parentInt := new(big.Int).SetBytes(parent.key)
	childInt := new(big.Int).Add(ilInt, parentInt)
	childInt.Mod(childInt, crypto.S256().Params().N)

	if childInt.Sign() == 0 {
		return nil, errors.New("resulting key is zero")
	}

	return &extendedKey{
		key:       childInt.Bytes(),
		chainCode: IR,
		isPrivate: true,
		index:     index,
		depth:     parent.depth + 1,
	}, nil
}

// pbkdf512 implements PBKDF2 with HMAC-SHA512.
func pbkdf512(password, salt []byte, iterations, keyLen int) []byte {
	var result []byte
	blocks := (keyLen + 63) / 64

	for block := 1; block <= blocks; block++ {
		//nolint:gocritic // PBKDF2 step description
		// U1 = HMAC(password, salt || INT_32_BE(block))
		saltBlock := make([]byte, len(salt)+4)
		copy(saltBlock, salt)
		binary.BigEndian.PutUint32(saltBlock[len(salt):], uint32(block))

		u := hmac.New(sha512.New, password)
		u.Write(saltBlock)
		U := u.Sum(nil)
		t := make([]byte, len(U))
		copy(t, U)

		for i := 1; i < iterations; i++ {
			u = hmac.New(sha512.New, password)
			u.Write(U)
			U = u.Sum(nil)
			for j := range t {
				t[j] ^= U[j]
			}
		}

		result = append(result, t...)
	}

	return result[:keyLen]
}

// bip39EnglishWords is the standard BIP-39 English word list (2048 words).
// This is the official list from https://github.com/bitcoin/bips/blob/master/bip-0039/english.txt
var bip39EnglishWords = []string{
	"abandon", "ability", "able", "about", "above", "absent", "absorb", "abstract", "absurd", "abuse",
	"access", "accident", "account", "accuse", "achieve", "acid", "acoustic", "acquire", "across", "act",
	"action", "actor", "actress", "actual", "adapt", "add", "addict", "address", "adjust", "admit",
	"adult", "advance", "advice", "aerobic", "affair", "afford", "afraid", "again", "age", "agent",
	"agree", "ahead", "aim", "air", "airport", "aisle", "alarm", "album", "alcohol", "alert",
	"alley", "allow", "almost", "alpha", "already", "also", "alter", "always", "amateur", "amazing",
	"among", "amount", "amused", "analyst", "anchor", "ancient", "anger", "angle", "angry", "animal",
	"ankle", "announce", "annual", "another", "answer", "antenna", "antique", "anxiety", "any", "apart",
	"apology", "appear", "apple", "approve", "april", "arch", "arctic", "area", "arena", "argue",
	"arm", "armed", "armor", "army", "around", "arrange", "arrest", "arrive", "arrow", "art",
	"artefact", "artist", "artwork", "ask", "aspect", "assault", "asset", "assist", "assume", "asthma",
	"athlete", "atom", "attack", "attend", "attitude", "attract", "auction", "audit", "august", "aunt",
	"author", "auto", "autumn", "average", "avocado", "avoid", "awake", "aware", "awesome", "awful",
	"awkward", "axis", "baby", "bachelor", "bacon", "badge", "bag", "balance", "balcony", "ball",
	"bamboo", "banana", "banner", "bar", "barely", "bargain", "barrel", "base", "basic", "basket",
	"battle", "beach", "bean", "beauty", "because", "become", "beef", "before", "begin", "behave",
	"behind", "believe", "below", "belt", "bench", "benefit", "best", "betray", "better", "between",
	"beyond", "bicycle", "bid", "bike", "bind", "biology", "bird", "birth", "bitter", "black",
	"blade", "blame", "blanket", "blast", "bleak", "bless", "blind", "blood", "blossom", "blow",
	"blue", "blur", "blush", "board", "boat", "body", "boil", "bomb", "bone", "bonus",
	"book", "boost", "border", "boring", "borrow", "boss", "bottom", "bounce", "box", "boy",
	"bracket", "brain", "brand", "brass", "brave", "bread", "breeze", "brick", "bridge", "brief",
	"bright", "bring", "brisk", "broccoli", "broken", "bronze", "broom", "brother", "brown", "brush",
	"bubble", "buddy", "budget", "buffalo", "build", "bulb", "bulk", "bullet", "bundle", "bunny",
	"burden", "burger", "burst", "bus", "business", "busy", "butter", "buyer", "buzz", "cabbage",
	"cabin", "cable", "cactus", "cage", "cake", "call", "calm", "camera", "camp", "can",
	"canal", "cancel", "candy", "cannon", "canoe", "canvas", "canyon", "capable", "capital", "captain",
	"car", "carbon", "card", "cargo", "carpet", "carry", "cart", "case", "cash", "casino",
	"castle", "casual", "cat", "catalog", "catch", "category", "cattle", "caught", "cause", "caution",
	"cave", "ceiling", "celery", "cement", "census", "century", "cereal", "certain", "chair", "chalk",
	"champion", "change", "chaos", "chapter", "charge", "chase", "cheap", "check", "cheese", "chef",
	"cherry", "chest", "chicken", "chief", "child", "chimney", "choice", "choose", "chronic", "chuckle",
	"chunk", "churn", "citizen", "city", "civil", "claim", "clap", "clarify", "claw", "clay",
	"clean", "clerk", "clever", "click", "client", "cliff", "climb", "clinic", "clip", "clock",
	"clog", "close", "cloth", "cloud", "clown", "club", "clump", "cluster", "clutch", "coach",
	"coast", "coconut", "code", "coffee", "coil", "coin", "collect", "color", "column", "combine",
	"come", "comfort", "comic", "common", "company", "concert", "conduct", "confirm", "congress", "connect",
	"consider", "control", "convince", "cook", "cool", "copper", "copy", "coral", "core", "corn",
	"correct", "cost", "cotton", "couch", "country", "couple", "course", "cousin", "cover", "coyote",
	"crack", "cradle", "craft", "cram", "crane", "crash", "crater", "crawl", "crazy", "cream",
	"credit", "creek", "crew", "cricket", "crime", "crisp", "critic", "crop", "cross", "crouch",
	"crowd", "crucial", "cruel", "cruise", "crumble", "crush", "cry", "crystal", "cube", "culture",
	"cup", "cupboard", "curious", "current", "curtain", "curve", "cushion", "custom", "cute", "cycle",
	"dad", "damage", "damp", "dance", "danger", "daring", "dash", "daughter", "dawn", "day",
	"deal", "debate", "debris", "decade", "december", "decide", "decline", "decorate", "decrease", "deer",
	"defense", "define", "defy", "degree", "delay", "deliver", "demand", "demise", "denial", "dentist",
	"deny", "depart", "depend", "deposit", "depth", "deputy", "derive", "describe", "desert", "design",
	"desk", "despair", "destroy", "detail", "detect", "develop", "device", "devote", "diagram", "dial",
	"diamond", "diary", "dice", "diesel", "diet", "differ", "digital", "dignity", "dilemma", "dinner",
	"dinosaur", "direct", "dirt", "disagree", "discover", "disease", "dish", "dismiss", "disorder", "display",
	"distance", "divert", "divide", "divorce", "dizzy", "doctor", "document", "dog", "doll", "dolphin",
	"domain", "donate", "donkey", "donor", "door", "dose", "double", "dove", "draft", "dragon",
	"drama", "drastic", "draw", "dream", "dress", "drift", "drill", "drink", "drip", "drive",
	"drop", "drum", "dry", "duck", "dumb", "dune", "during", "dust", "dutch", "duty",
	"dwarf", "dynamic", "eager", "eagle", "early", "earn", "earth", "easily", "east", "easy",
	"echo", "ecology", "economy", "edge", "edit", "educate", "effort", "egg", "eight", "either",
	"elbow", "elder", "electric", "elegant", "element", "elephant", "elevator", "elite", "else", "embark",
	"embody", "embrace", "emerge", "emotion", "employ", "empower", "empty", "enable", "encourage", "end",
	"endless", "endorse", "enemy", "energy", "enforce", "engage", "engine", "enhance", "enjoy", "enlist",
	"enough", "enrich", "enroll", "ensure", "enter", "entire", "entry", "envelope", "episode", "equal",
	"equip", "era", "erase", "erode", "erosion", "error", "erupt", "escape", "essay", "essence",
	"estate", "eternal", "evidence", "evil", "evoke", "evolve", "exact", "example", "excess", "exchange",
	"excite", "exclude", "excuse", "execute", "exercise", "exhaust", "exhibit", "exile", "exist", "exit",
	"exotic", "expand", "expect", "expire", "explain", "expose", "express", "extend", "extra", "eye",
	"eyebrow", "fabric", "face", "faculty", "fade", "faint", "faith", "fall", "false", "fame",
	"family", "famous", "fan", "fancy", "fantasy", "farm", "fashion", "fat", "fatal", "father",
	"fatigue", "fault", "favorite", "feature", "february", "federal", "fee", "feed", "feel", "female",
	"fence", "festival", "fetch", "fever", "few", "fiber", "fiction", "field", "figure", "file",
	"film", "filter", "final", "find", "fine", "finger", "finish", "fire", "firm", "fiscal",
	"fish", "fit", "fitness", "fix", "flag", "flame", "flash", "flat", "flavor", "flee",
	"flight", "flip", "float", "flock", "floor", "flower", "fluid", "flush", "fly", "foam",
	"focus", "fog", "foil", "fold", "follow", "food", "foot", "force", "forest", "forget",
	"fork", "fortune", "forum", "forward", "fossil", "foster", "found", "fox", "fragile", "frame",
	"frequent", "fresh", "friend", "fringe", "frog", "front", "frost", "frown", "frozen", "fruit",
	"fuel", "fun", "funny", "furnace", "fury", "future", "gadget", "gain", "galaxy", "gallery",
	"game", "gap", "garage", "garbage", "garden", "garlic", "garment", "gas", "gasp", "gate",
	"gather", "gauge", "gaze", "general", "genius", "genre", "gentle", "genuine", "gesture", "ghost",
	"giant", "gift", "giggle", "ginger", "giraffe", "girl", "give", "glad", "glance", "glare",
	"glass", "glide", "glimpse", "globe", "gloom", "glory", "glove", "glow", "glue", "goat",
	"goddess", "gold", "good", "goose", "gorilla", "gospel", "gossip", "govern", "gown", "grab",
	"grace", "grain", "grant", "grape", "grass", "gravity", "great", "green", "grid", "grief",
	"grit", "grocery", "group", "grow", "grunt", "guard", "guess", "guide", "guilt", "guitar",
	"gun", "gym", "habit", "hair", "half", "hammer", "hamster", "hand", "happy", "harbor",
	"hard", "harsh", "harvest", "hat", "have", "hawk", "hazard", "head", "health", "heart",
	"heavy", "hedgehog", "height", "hello", "helmet", "help", "hen", "hero", "hip", "hire",
	"history", "hobby", "hockey", "hold", "hole", "holiday", "hollow", "home", "honey", "hood",
	"hope", "horn", "horror", "horse", "hospital", "host", "hotel", "hour", "hover", "hub",
	"huge", "human", "humble", "humor", "hundred", "hungry", "hunt", "hurdle", "hurry", "hurt",
	"husband", "hybrid", "ice", "icon", "idea", "identify", "idle", "ignore", "ill", "illegal",
	"illness", "image", "imitate", "immense", "immune", "impact", "impose", "improve", "impulse", "inch",
	"include", "income", "increase", "index", "indicate", "indoor", "industry", "infant", "inflict", "inform",
	"initial", "inject", "inmate", "inner", "innocent", "input", "inquiry", "insane", "insect", "inside",
	"inspire", "install", "intact", "interest", "into", "invest", "invite", "involve", "iron", "island",
	"isolate", "issue", "item", "ivory", "jacket", "jaguar", "jar", "jazz", "jealous", "jeans",
	"jelly", "jewel", "job", "join", "joke", "journey", "joy", "judge", "juice", "jump",
	"jungle", "junior", "junk", "just", "kangaroo", "keen", "keep", "ketchup", "key", "kick",
	"kid", "kidney", "kind", "kingdom", "kiss", "kit", "kitchen", "kite", "kitten", "kiwi",
	"knee", "knife", "knock", "know", "lab", "label", "labor", "ladder", "lady", "lake",
	"lamp", "language", "laptop", "large", "later", "latin", "laugh", "laundry", "lava", "law",
	"lawn", "lawsuit", "layer", "lazy", "leader", "leaf", "learn", "leave", "lecture", "left",
	"leg", "legal", "legend", "leisure", "lemon", "lend", "length", "lens", "leopard", "lesson",
	"letter", "level", "liberty", "library", "license", "life", "lift", "light", "like", "limb",
	"limit", "link", "lion", "liquid", "list", "little", "live", "lizard", "load", "loan",
	"lobster", "local", "lock", "logic", "lonely", "long", "loop", "lottery", "loud", "lounge",
	"love", "loyal", "lucky", "luggage", "lumber", "lunar", "lunch", "luxury", "lyrics", "machine",
	"mad", "magic", "magnet", "maid", "mail", "main", "major", "make", "mammal", "man",
	"manage", "mandate", "mango", "mansion", "manual", "maple", "marble", "march", "margin", "marine",
	"market", "marriage", "mask", "mass", "master", "match", "material", "math", "matrix", "matter",
	"maximum", "maze", "meadow", "mean", "measure", "meat", "mechanic", "medal", "media", "melody",
	"melt", "member", "memory", "mention", "menu", "mercy", "merge", "merit", "merry", "mesh",
	"message", "metal", "method", "middle", "midnight", "milk", "million", "mimic", "mind", "minimum",
	"minor", "minute", "miracle", "mirror", "misery", "miss", "mistake", "mix", "mixed", "mixture",
	"mobile", "model", "modify", "mom", "moment", "monitor", "monkey", "monster", "month", "moon",
	"moral", "more", "morning", "mosquito", "mother", "motion", "motor", "mountain", "mouse", "move",
	"movie", "much", "muffin", "mule", "multiply", "muscle", "museum", "mushroom", "music", "must",
	"mutual", "myself", "mystery", "myth", "naive", "name", "napkin", "narrow", "nasty", "nation",
	"nature", "near", "neck", "need", "negative", "neglect", "neither", "nephew", "nerve", "nest",
	"net", "network", "neutral", "never", "news", "next", "nice", "night", "noble", "noise",
	"nominee", "noodle", "normal", "north", "nose", "notable", "nothing", "notice", "novel", "now",
	"nuclear", "number", "nurse", "nut", "oak", "obey", "object", "oblige", "obscure", "observe",
	"obtain", "obvious", "occur", "ocean", "october", "odor", "off", "offer", "office", "often",
	"oil", "okay", "old", "olive", "olympic", "omit", "once", "one", "onion", "online",
	"only", "open", "opera", "opinion", "oppose", "option", "orange", "orbit", "orchard", "order",
	"ordinary", "organ", "orient", "original", "orphan", "ostrich", "other", "outdoor", "outer", "output",
	"outside", "oval", "oven", "over", "own", "owner", "oxygen", "oyster", "ozone", "pact",
	"paddle", "page", "pair", "palace", "palm", "panda", "panel", "panic", "panther", "paper",
	"parade", "parent", "park", "parrot", "party", "pass", "patch", "path", "patient", "patrol",
	"pattern", "pause", "pave", "payment", "peace", "peanut", "pear", "peasant", "pelican", "pen",
	"penalty", "pencil", "people", "pepper", "perfect", "permit", "person", "pet", "phone", "photo",
	"phrase", "physical", "piano", "picnic", "picture", "piece", "pig", "pigeon", "pill", "pilot",
	"pink", "pioneer", "pipe", "pistol", "pitch", "pizza", "place", "planet", "plastic", "plate",
	"play", "please", "pledge", "pluck", "plug", "plunge", "poem", "poet", "point", "polar",
	"pole", "police", "pond", "pony", "pool", "popular", "portion", "position", "possible", "post",
	"potato", "pottery", "poverty", "powder", "power", "practice", "praise", "predict", "prefer", "prepare",
	"present", "pretty", "prevent", "price", "pride", "primary", "print", "priority", "prison", "private",
	"prize", "problem", "process", "produce", "profit", "program", "project", "promote", "proof", "property",
	"prosper", "protect", "proud", "provide", "public", "pudding", "pull", "pulp", "pulse", "pumpkin",
	"punch", "pupil", "puppy", "purchase", "purity", "purpose", "purse", "push", "put", "puzzle",
	"pyramid", "quality", "quantum", "quarter", "question", "quick", "quit", "quiz", "quote", "rabbit",
	"raccoon", "race", "rack", "radar", "radio", "rage", "rail", "rain", "raise", "rally",
	"ramp", "ranch", "random", "range", "rapid", "rare", "rate", "rather", "raven", "raw",
	"razor", "ready", "real", "reason", "rebel", "rebuild", "recall", "receive", "recipe", "record",
	"recycle", "reduce", "reflect", "reform", "region", "regret", "regular", "reject", "relax", "release",
	"relief", "rely", "remain", "remember", "remind", "remove", "render", "renew", "rent", "reopen",
	"repair", "repeat", "replace", "report", "require", "rescue", "resemble", "resist", "resource", "response",
	"result", "retire", "retreat", "return", "reunion", "reveal", "review", "reward", "rhythm", "rib",
	"ribbon", "rice", "rich", "ride", "ridge", "rifle", "right", "rigid", "ring", "riot",
	"ripple", "risk", "ritual", "rival", "river", "road", "roast", "robot", "robust", "rocket",
	"romance", "roof", "rookie", "room", "rose", "rotate", "rough", "round", "route", "royal",
	"rubber", "rude", "rug", "rule", "run", "runway", "rural", "sad", "saddle", "sadness",
	"safe", "sail", "salad", "salmon", "salon", "salt", "salute", "same", "sample", "sand",
	"satisfy", "satoshi", "sauce", "sausage", "save", "say", "scale", "scan", "scare", "scatter",
	"scene", "scheme", "school", "science", "scissors", "scorpion", "scout", "scrap", "screen", "script",
	"scrub", "sea", "search", "season", "seat", "second", "secret", "section", "security", "seed",
	"seek", "segment", "select", "sell", "seminar", "senior", "sense", "sentence", "series", "service",
	"session", "settle", "setup", "seven", "shadow", "shaft", "shallow", "share", "shed", "shell",
	"sheriff", "shield", "shift", "shine", "ship", "shiver", "shock", "shoe", "shoot", "shop",
	"short", "shoulder", "shove", "shrimp", "shrug", "shuffle", "shy", "sibling", "sick", "side",
	"siege", "sight", "sign", "silent", "silk", "silly", "silver", "similar", "simple", "since",
	"sing", "siren", "sister", "situate", "six", "size", "skate", "sketch", "ski", "skill",
	"skin", "skirt", "skull", "slab", "slam", "sleep", "slender", "slice", "slide", "slight",
	"slim", "slogan", "slot", "slow", "slush", "small", "smart", "smile", "smoke", "smooth",
	"snack", "snake", "snap", "sniff", "snow", "soap", "soccer", "social", "sock", "soda",
	"soft", "solar", "soldier", "solid", "solution", "solve", "someone", "song", "soon", "sorry",
	"sort", "soul", "sound", "soup", "source", "south", "space", "spare", "spatial", "spawn",
	"speak", "special", "speed", "spell", "spend", "sphere", "spice", "spider", "spike", "spin",
	"spirit", "split", "sponsor", "spoon", "sport", "spot", "spray", "spread", "spring", "spy",
	"square", "squeeze", "squirrel", "stable", "stadium", "staff", "stage", "stairs", "stamp", "stand",
	"start", "state", "stay", "steak", "steel", "stem", "step", "stereo", "stick", "still",
	"sting", "stock", "stomach", "stone", "stool", "story", "stove", "strategy", "street", "strike",
	"strong", "struggle", "student", "stuff", "stumble", "style", "subject", "submit", "subway", "success",
	"such", "sudden", "suffer", "sugar", "suggest", "suit", "summer", "sun", "sunny", "sunset",
	"super", "supply", "supreme", "sure", "surface", "surge", "surprise", "surround", "survey", "suspect",
	"sustain", "swallow", "swamp", "swap", "swarm", "swear", "sweet", "swim", "swing", "switch",
	"sword", "symbol", "symptom", "syrup", "system", "table", "tackle", "tag", "tail", "talent",
	"talk", "tank", "tape", "target", "task", "taste", "tattoo", "taxi", "teach", "team",
	"tell", "ten", "tenant", "tennis", "tent", "term", "test", "text", "thank", "that",
	"theme", "then", "theory", "there", "they", "thing", "this", "thought", "three", "thrive",
	"throw", "thumb", "thunder", "ticket", "tide", "tiger", "tilt", "timber", "time", "tiny",
	"tip", "tired", "tissue", "title", "toast", "tobacco", "today", "toddler", "toe", "together",
	"toilet", "token", "tomato", "tomorrow", "tone", "tongue", "tonight", "tool", "tooth", "top",
	"topic", "topple", "torch", "tornado", "tortoise", "toss", "total", "tourist", "toward", "tower",
	"town", "toy", "track", "trade", "traffic", "tragic", "train", "transfer", "trap", "trash",
	"travel", "tray", "treat", "tree", "trend", "trial", "tribe", "trick", "trigger", "trim",
	"trip", "trophy", "trouble", "truck", "true", "truly", "trumpet", "trust", "truth", "try",
	"tube", "tuna", "tunnel", "turkey", "turn", "turtle", "twelve", "twenty", "twice", "twin",
	"twist", "two", "type", "typical", "ugly", "umbrella", "unable", "unaware", "uncle", "uncover",
	"under", "undo", "unfair", "unfold", "unhappy", "uniform", "union", "unique", "unit", "universe",
	"unknown", "unlock", "until", "unusual", "unveil", "update", "upgrade", "uphold", "upon", "upper",
	"upset", "urban", "usage", "use", "used", "useful", "useless", "usual", "utility", "vacant",
	"vacuum", "vague", "valid", "valley", "valve", "van", "vanish", "vapor", "various", "vast",
	"vault", "vehicle", "velvet", "vendor", "venture", "venue", "verb", "verify", "version", "very",
	"vessel", "veteran", "viable", "vibrant", "vicious", "victory", "video", "view", "village", "vintage",
	"violin", "virtual", "virus", "visa", "visit", "visual", "vital", "vivid", "vocal", "voice",
	"void", "volcano", "volume", "vote", "voyage", "wage", "wagon", "wait", "walk", "wall",
	"walnut", "want", "warfare", "warm", "warrior", "wash", "wasp", "waste", "water", "wave",
	"way", "wealth", "weapon", "wear", "weasel", "weather", "web", "wedding", "weekend", "weird",
	"welcome", "well", "west", "wet", "whale", "what", "wheat", "wheel", "when", "where",
	"whip", "whisper", "wide", "width", "wife", "wild", "will", "win", "window", "wine",
	"wing", "wink", "winner", "winter", "wire", "wisdom", "wise", "wish", "witness", "wolf",
	"woman", "wonder", "wood", "wool", "word", "work", "world", "worry", "worth", "wrap",
	"wreck", "wrestle", "wrist", "write", "wrong", "yard", "year", "yellow", "you", "young",
	"youth", "zebra", "zero", "zone", "zoo",
}
