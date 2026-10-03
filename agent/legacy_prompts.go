package agent

import (
	"crypto/sha256"
	"encoding/hex"
)

// Frozen SHA-256 fingerprints of shipped defaults at and before 160fe13.
// Keep prompt bodies out of source: operator edits must survive English upgrades.
var legacyPromptSHA256 = map[string][]string{
	"assistant_fallback": {
		"a179c990bff1ab4e0a6f326945a1c44036ef4c8a54c7c5b635965f6cefdc9f0a",
	},
	"auto": {
		"b2a71b82243607337b19f0642317fed88c144d536412d80eaaf90807f2905a1d",
	},
	"goals": {
		"72a7a0159443b41dde29dfba8a3de6a2c8a15b273935495a7b88561fa708aa68",
		"9c51639015a0a648d08b52d0fa46b32eb432bd0eb4231d75b0ed5bf76ee235ca",
	},
	"mainagent": {
		"12bb69ee0886c4fb9beb936d882021a91104860f1da69a5e3eda3d9e345cd399",
		"33a2934883fefbf9b41a674921c77027e6d6c9e8f867d150e5f87811f9c9c1df",
		"4938dfffee46bf983aa9291e149159eb273999056188b74f666a2173926f9074",
		"a1d251781a6b411e68134ca03d4ad4596a21d221152562d2b6d067e7d27aebff",
		"e9093a04fd92d72d307034168643195461a53c7ef1d1befe80125e00a98c1fc3",
	},
	"pentest": {
		"7c1664f8061295ea70a4dc5977d98c599844ef8fcfedf0e501f8edc7afa35326",
		"a54bd5713b1c121d0f2ff3a72e0304820b1368c4b4b8159d5d651895663ac652",
	},
	"planner": {
		"01497d1bffc54d5cb041bfe89dd0c0d314a0a6b0e311b1ee2f1bdc18121e4f51",
		"14e163f4a44da9fee20a214048a607adc3659b2025275569eb512532ca8c15d5",
		"18e7de64cf5e6528b9b852306f12ef73089efd574bb0984e34f49133c141f61c",
		"1ca382cd6c964e948af8560f5cbad5f78fa9f4b36031ea46d91eddfc5054ecfe",
		"3c0c2b90eee4a1a559efc750c7551e9882bd98caa531f90c0f0a51398483701d",
		"49ac9eeaf59f29e31d5ec52d2b0fb977c157e9279b28657629c2a4d1db7e80b4",
		"5736b2fd9d264494ee5a0d0eac8d9440e7b85b0a0e3874d84eea48851dcedcf8",
		"6c3c694a0c9e21aa6d4b5fffbe1d07affa395c3173f82f2e3decb4c9eca1c91e",
		"82672b54620846e389d6cf6c63d79ebb294a7ceea52d8a479424621825d0f84c",
		"87e60f746a60695bb4fcf00a90fba19a6c5b58a1050168b4a72e2a0d9de1671b",
		"e0e2b7b9ff43a8673404b4eacb77ab4e76780589dcd0fb6d6744602334aea17a",
	},
	"reporter": {
		"3743a8c374b45a9b24390457ecdbd50465693912a4e0b3c77107056bda5b389b",
		"86ad261f9773aec97a8ea1c7040f5efcf5142da53ef1ff49bf1cfd1d227c10cc",
		"8ba98208f6c5f2ccb103c88ef13239c4264733400c0e3dfec96d3a3572d78d39",
	},
	"retester": {
		"d08326aa9fbd9a1367e716cf7ef46533e9096e9a56493f77d364efbf696f7fe0",
	},
	"worker": {
		"261914723f71ca5123e2c310d42a3f34ac8594b32b6d479003c373f282fa83f9",
		"48dcd8a04e204d29acddffde6e6213e0f35b369852f8e8233fff5cde96fecc89",
		"6450be9f636f580a8737faf6d1f59e137abfd60f8bad35a86ed2f9b379ea4883",
		"64cd6524f2f6cabd55b5fcd95612d4db1a340200738c7f83ae768874273728ce",
		"7f13bf6d9f3586663a99b45fa8200dd9e25258f73cae5f13c876fdee2f2dbbda",
		"80fbb1c610dd57e266c20b9145793339eee540b809f6db8f5336261b45879f08",
		"9f40b972a3383c6299fc39889a165414f4960633f8e160e4432ed28558c3ca94",
		"b3a50039c00da19800858c58bdbb2ed540fa41a10ee19339b7a34ef521ebdca3",
		"b43639b21c6ab4e32bb48ecdcfe09e43c7dde4554d262c36c3256b3064372590",
		"c3fe3015fb22020c5afbebafcdd100e4fa4ec307319ecc6fd119003be4579b3c",
		"d69f9f3c9375b64640b95d14e84816ae4d80227c9faf74d936c6073205dbab5a",
		"de84975cbf8edf9665a0d77b39f7be32dfd75dbb119c516ae4af040af2bcccf2",
	},
}

// LegacyPromptDigests returns a copy of the recognized historical defaults.
func LegacyPromptDigests(key string) []string {
	return append([]string(nil), legacyPromptSHA256[key]...)
}

// IsLegacyPromptDefault compares exact bytes; whitespace edits are customizations.
func IsLegacyPromptDefault(key, body string) bool {
	digest := sha256.Sum256([]byte(body))
	sum := hex.EncodeToString(digest[:])
	for _, known := range legacyPromptSHA256[key] {
		if sum == known {
			return true
		}
	}
	return false
}
