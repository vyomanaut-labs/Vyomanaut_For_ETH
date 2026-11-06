package arkiv

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

// File manifests and tombstones.
//
// A manifest says "this file was registered with this pointer and these
// shards". It never carries the pointer itself, the owner, the size or the
// name: the pointer key is derived from the owner's passphrase, so publishing
// the ciphertext would allow offline guessing. Only SHA-256 of the stored
// pointer (nonce || ciphertext || tag) is published.
//
// shard_root commits to the file's shards: an RFC 6962 tree (built with
// receiptleaf.MerkleRoot) over the leaves
//
//	SHA-256(0x00 || segment_index (4, BE) || shard_index (2, BE) || chunk_id (32))
//
// in (segment_index, shard_index) order.
//
// A tombstone is published once the coordinator sees the file deleted.

var (
	// ErrBadManifest means a published manifest or tombstone does not verify.
	ErrBadManifest = errors.New("arkiv: invalid file manifest")

	domainFileManifest  = domainTag("vyomanaut/arkiv/file_manifest/v1")
	domainFileTombstone = domainTag("vyomanaut/arkiv/file_tombstone/v1")
)

const (
	manifestBodySchema  = "vyomanaut.arkiv.file_manifest.v1"
	tombstoneBodySchema = "vyomanaut.arkiv.file_tombstone.v1"

	// Attribute names used only by manifests.
	AttrPointerHash = "pointer_sha256"
	AttrShardRoot   = "shard_root"
	AttrSegments    = "segments"
	AttrShards      = "shards"
	AttrRSK         = "rs_k"
	AttrRSN         = "rs_n"
	AttrRegistered  = "registered_at"
	AttrDeleted     = "deleted_seen_at"
)

// Shard is one RS shard of a file.
type Shard struct {
	SegmentIndex uint32
	ShardIndex   uint16
	ChunkID      [32]byte
}

// FileManifest is what the coordinator publishes about one registered file.
type FileManifest struct {
	FileID        [16]byte
	PointerSHA256 [32]byte
	ShardRoot     [32]byte
	Segments      uint32
	Shards        uint32
	RSK           uint8
	RSN           uint8
	RegisteredAt  time.Time
}

// PointerHash returns SHA-256(nonce || ciphertext || tag) of a stored pointer.
func PointerHash(nonce, ciphertext, tag []byte) [32]byte {
	h := sha256.New()
	h.Write(nonce)
	h.Write(ciphertext)
	h.Write(tag)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// ShardRoot returns the shard tree root of a file's shards. The shards are
// sorted into (segment_index, shard_index) order first.
func ShardRoot(shards []Shard) [32]byte {
	s := append([]Shard(nil), shards...)
	sort.Slice(s, func(i, j int) bool {
		if s[i].SegmentIndex != s[j].SegmentIndex {
			return s[i].SegmentIndex < s[j].SegmentIndex
		}
		return s[i].ShardIndex < s[j].ShardIndex
	})
	leaves := make([][32]byte, len(s))
	for i, sh := range s {
		buf := make([]byte, 0, 1+4+2+32)
		buf = append(buf, receiptleaf.LeafPrefix)
		buf = binary.BigEndian.AppendUint32(buf, sh.SegmentIndex)
		buf = binary.BigEndian.AppendUint16(buf, sh.ShardIndex)
		buf = append(buf, sh.ChunkID[:]...)
		leaves[i] = sha256.Sum256(buf)
	}
	return receiptleaf.MerkleRoot(leaves)
}

// FileManifestSigningBytes is the fixed-layout message the coordinator signs
// (big endian, 160 bytes):
//
//	32 domain tag "vyomanaut/arkiv/file_manifest/v1" (zero padded)
//	16 file_id
//	32 pointer_sha256
//	32 shard_root
//	 4 segments
//	 4 shards
//	 1 rs_k
//	 1 rs_n
//	 8 registered_at (Unix microseconds)
//	30 zero (reserved)
func FileManifestSigningBytes(m FileManifest) []byte {
	const size = 160
	buf := make([]byte, 0, size)
	buf = append(buf, domainFileManifest[:]...)
	buf = append(buf, m.FileID[:]...)
	buf = append(buf, m.PointerSHA256[:]...)
	buf = append(buf, m.ShardRoot[:]...)
	buf = binary.BigEndian.AppendUint32(buf, m.Segments)
	buf = binary.BigEndian.AppendUint32(buf, m.Shards)
	buf = append(buf, m.RSK, m.RSN)
	buf = binary.BigEndian.AppendUint64(buf, uint64(m.RegisteredAt.UnixMicro()))
	return append(buf, make([]byte, size-len(buf))...)
}

// TombstoneSigningBytes is the fixed-layout message signed for a tombstone
// (big endian, 64 bytes): 32 domain tag, 16 file_id, 8 deleted_seen_at (Unix
// microseconds), 8 zero.
func TombstoneSigningBytes(fileID [16]byte, seen time.Time) []byte {
	const size = 64
	buf := make([]byte, 0, size)
	buf = append(buf, domainFileTombstone[:]...)
	buf = append(buf, fileID[:]...)
	buf = binary.BigEndian.AppendUint64(buf, uint64(seen.UnixMicro()))
	return append(buf, make([]byte, size-len(buf))...)
}

type manifestBody struct {
	Schema          string `json:"schema"`
	FileID          string `json:"file_id"`
	PointerSHA256   string `json:"pointer_sha256"`
	ShardRoot       string `json:"shard_root"`
	Segments        uint32 `json:"segments"`
	Shards          uint32 `json:"shards"`
	RSK             uint8  `json:"rs_k"`
	RSN             uint8  `json:"rs_n"`
	RegisteredAtUS  int64  `json:"registered_at_us"`
	CoordKey        string `json:"coord_key"`
	CoordSig        string `json:"coord_sig"`
	DeletedSeenAtUS int64  `json:"deleted_seen_at_us,omitempty"`
}

func signer(key ed25519.PrivateKey) (ed25519.PublicKey, error) {
	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("arkiv: the signing key is not an Ed25519 key")
	}
	return pub, nil
}

// FileManifestAttributes signs the manifest and returns its entity attributes.
func FileManifestAttributes(m FileManifest, key ed25519.PrivateKey, appTag string) ([]Attribute, int, error) {
	pub, err := signer(key)
	if err != nil {
		return nil, 0, err
	}
	sig := ed25519.Sign(key, FileManifestSigningBytes(m))
	fileID := receiptleaf.FormatUUID(m.FileID)
	body, err := json.Marshal(manifestBody{
		Schema: manifestBodySchema, FileID: fileID,
		PointerSHA256: hex.EncodeToString(m.PointerSHA256[:]), ShardRoot: hex.EncodeToString(m.ShardRoot[:]),
		Segments: m.Segments, Shards: m.Shards, RSK: m.RSK, RSN: m.RSN,
		RegisteredAtUS: m.RegisteredAt.UnixMicro(),
		CoordKey:       hex.EncodeToString(pub), CoordSig: hex.EncodeToString(sig),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("arkiv: manifest body: %w", err)
	}
	if appTag == "" {
		appTag = DefaultAppTag
	}
	attrs, err := buildAttrs(
		str(AttrApp, appTag, true),
		str(AttrKind, KindFileManifest, true),
		u64(AttrSchema, SchemaVersion, true),
		str(AttrFileID, fileID, true),
		str(AttrPointerHash, hex.EncodeToString(m.PointerSHA256[:]), true),
		str(AttrShardRoot, hex.EncodeToString(m.ShardRoot[:]), true),
		u64(AttrSegments, uint64(m.Segments), true),
		u64(AttrShards, uint64(m.Shards), true),
		u64(AttrRSK, uint64(m.RSK), true),
		u64(AttrRSN, uint64(m.RSN), true),
		u64(AttrRegistered, uint64(m.RegisteredAt.Unix()), true),
		str(AttrCoordKey, hex.EncodeToString(pub), true),
		str(AttrCoordSig, hex.EncodeToString(sig), false),
		bytesAttr(AttrBody, body),
	)
	return attrs, len(body), err
}

// TombstoneAttributes signs a tombstone and returns its entity attributes.
func TombstoneAttributes(fileID [16]byte, seen time.Time, key ed25519.PrivateKey, appTag string) ([]Attribute, int, error) {
	pub, err := signer(key)
	if err != nil {
		return nil, 0, err
	}
	sig := ed25519.Sign(key, TombstoneSigningBytes(fileID, seen))
	id := receiptleaf.FormatUUID(fileID)
	body, err := json.Marshal(manifestBody{
		Schema: tombstoneBodySchema, FileID: id, DeletedSeenAtUS: seen.UnixMicro(),
		CoordKey: hex.EncodeToString(pub), CoordSig: hex.EncodeToString(sig),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("arkiv: tombstone body: %w", err)
	}
	if appTag == "" {
		appTag = DefaultAppTag
	}
	attrs, err := buildAttrs(
		str(AttrApp, appTag, true),
		str(AttrKind, KindFileTombstone, true),
		u64(AttrSchema, SchemaVersion, true),
		str(AttrFileID, id, true),
		u64(AttrDeleted, uint64(seen.Unix()), true),
		str(AttrCoordKey, hex.EncodeToString(pub), true),
		str(AttrCoordSig, hex.EncodeToString(sig), false),
		bytesAttr(AttrBody, body),
	)
	return attrs, len(body), err
}

// DecodeFileManifest reads a manifest body and verifies its signature under the
// key it names. The caller checks that key is the trusted coordinator key.
func DecodeFileManifest(body []byte) (FileManifest, ed25519.PublicKey, error) {
	var raw manifestBody
	if err := json.Unmarshal(body, &raw); err != nil || raw.Schema != manifestBodySchema {
		return FileManifest{}, nil, fmt.Errorf("%w: unreadable body", ErrBadManifest)
	}
	var m FileManifest
	var err error
	if m.FileID, err = receiptleaf.ParseUUID(raw.FileID); err != nil {
		return m, nil, fmt.Errorf("%w: %v", ErrBadManifest, err)
	}
	if err = hex32(&m.PointerSHA256, raw.PointerSHA256); err != nil {
		return m, nil, err
	}
	if err = hex32(&m.ShardRoot, raw.ShardRoot); err != nil {
		return m, nil, err
	}
	m.Segments, m.Shards, m.RSK, m.RSN = raw.Segments, raw.Shards, raw.RSK, raw.RSN
	m.RegisteredAt = time.UnixMicro(raw.RegisteredAtUS).UTC()
	pub, err := verifySig(raw.CoordKey, raw.CoordSig, FileManifestSigningBytes(m))
	return m, pub, err
}

// DecodeTombstone reads a tombstone body and verifies its signature.
func DecodeTombstone(body []byte) (fileID [16]byte, seen time.Time, pub ed25519.PublicKey, err error) {
	var raw manifestBody
	if err = json.Unmarshal(body, &raw); err != nil || raw.Schema != tombstoneBodySchema {
		return fileID, seen, nil, fmt.Errorf("%w: unreadable tombstone", ErrBadManifest)
	}
	if fileID, err = receiptleaf.ParseUUID(raw.FileID); err != nil {
		return fileID, seen, nil, fmt.Errorf("%w: %v", ErrBadManifest, err)
	}
	seen = time.UnixMicro(raw.DeletedSeenAtUS).UTC()
	pub, err = verifySig(raw.CoordKey, raw.CoordSig, TombstoneSigningBytes(fileID, seen))
	return fileID, seen, pub, err
}

func hex32(dst *[32]byte, s string) error {
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != len(dst) {
		return fmt.Errorf("%w: bad 32-byte hex field", ErrBadManifest)
	}
	copy(dst[:], raw)
	return nil
}

func verifySig(keyHex, sigHex string, msg []byte) (ed25519.PublicKey, error) {
	pub, err := hex.DecodeString(keyHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: bad coordinator key", ErrBadManifest)
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || !ed25519.Verify(pub, msg, sig) {
		return nil, fmt.Errorf("%w: the coordinator signature does not verify", ErrBadManifest)
	}
	return ed25519.PublicKey(pub), nil
}
