package backend_test

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"

	"wg-turn-client"
)

// ============================================================
// NewObfsConfig — режимы audio и video задают разные параметры
// ============================================================

func TestObfsConfig_AudioMode(t *testing.T) {
	cfg := core.NewObfsConfig("audio")
	if cfg.PayloadType != 111 {
		t.Errorf("audio PayloadType: got %d, want 111", cfg.PayloadType)
	}
	if cfg.PaddingMax != 24 {
		t.Errorf("audio PaddingMax: got %d, want 24", cfg.PaddingMax)
	}
	if cfg.SSRC == 0 {
		t.Error("SSRC should be random, got 0")
	}
}

func TestObfsConfig_VideoMode(t *testing.T) {
	cfg := core.NewObfsConfig("video")
	if cfg.PayloadType != 96 {
		t.Errorf("video PayloadType: got %d, want 96", cfg.PayloadType)
	}
	if cfg.PaddingMax != 60 {
		t.Errorf("video PaddingMax: got %d, want 60", cfg.PaddingMax)
	}
	if cfg.SSRC == 0 {
		t.Error("SSRC should be random, got 0")
	}
}

func TestObfsConfig_DefaultIsAudio(t *testing.T) {
	cfg := core.NewObfsConfig("")
	if cfg.PayloadType != 111 {
		t.Errorf("empty mode should default to audio, PayloadType: got %d, want 111", cfg.PayloadType)
	}
	if cfg.PaddingMax != 24 {
		t.Errorf("empty mode should default to audio, PaddingMax: got %d, want 24", cfg.PaddingMax)
	}
}

func TestObfsConfig_SSRCIsRandom(t *testing.T) {
	seen := make(map[uint32]bool)
	for i := 0; i < 50; i++ {
		cfg := core.NewObfsConfig("audio")
		if seen[cfg.SSRC] {
			t.Fatalf("SSRC collision after %d iterations", i)
		}
		seen[cfg.SSRC] = true
	}
}

// ============================================================
// Wrap + Unwrap roundtrip — оба режима корректно шифруют/дешифруют
// ============================================================

func TestObfsRoundtrip_AudioMode(t *testing.T) {
	testObfsRoundtrip(t, "audio", 128)
}

func TestObfsRoundtrip_VideoMode(t *testing.T) {
	testObfsRoundtrip(t, "video", 128)
}

func TestObfsRoundtrip_SmallPayload(t *testing.T) {
	testObfsRoundtrip(t, "audio", 1)
}

func TestObfsRoundtrip_LargePayload(t *testing.T) {
	testObfsRoundtrip(t, "video", 1400)
}

func testObfsRoundtrip(t *testing.T, mode string, payloadLen int) {
	t.Helper()

	key := make([]byte, 32)
	rand.Read(key)

	cfg := core.NewObfsConfig(mode)
	state := core.NewObfsState()
	payload := make([]byte, payloadLen)
	rand.Read(payload)

	// Wrap
	wire, err := core.ObfsWrapPacket(key, payload, cfg, state)
	if err != nil {
		t.Fatalf("ObfsWrapPacket: %v", err)
	}

	// Verify RTP header
	if (wire[0] >> 6) != 2 {
		t.Errorf("RTP version: got %d, want 2", wire[0]>>6)
	}
	gotPT := wire[1] & 0x7F
	if gotPT != cfg.PayloadType {
		t.Errorf("PayloadType in wire: got %d, want %d", gotPT, cfg.PayloadType)
	}

	// Unwrap
	dst := make([]byte, len(payload))
	n, err := core.ObfsUnwrapPacket(key, wire, dst)
	if err != nil {
		t.Fatalf("ObfsUnwrapPacket: %v", err)
	}
	if !bytes.Equal(payload, dst[:n]) {
		t.Errorf("roundtrip mismatch: input len=%d, output len=%d", payloadLen, n)
	}
}

// ============================================================
// Пакеты audio и video различаются по размеру и PayloadType
// ============================================================

func TestObfsPacketSize_Differs(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	payload := make([]byte, 120)
	rand.Read(payload)

	audioCfg := core.NewObfsConfig("audio")
	videoCfg := core.NewObfsConfig("video")

	audioState := core.NewObfsState()
	videoState := core.NewObfsState()

	// Заворачиваем много пакетов и смотрим на средний размер
	const trials = 200
	audioTotal := 0
	videoTotal := 0

	for i := 0; i < trials; i++ {
		aw, err := core.ObfsWrapPacket(key, payload, audioCfg, audioState)
		if err != nil {
			t.Fatal(err)
		}
		vw, err := core.ObfsWrapPacket(key, payload, videoCfg, videoState)
		if err != nil {
			t.Fatal(err)
		}
		audioTotal += len(aw)
		videoTotal += len(vw)
	}

	audioAvg := float64(audioTotal) / trials
	videoAvg := float64(videoTotal) / trials

	// PayloadType разный → RTP header байт[1] разный
	// PaddingMax разный: audio=24, video=60 → средний размер video пакетов должен быть больше
	t.Logf("audio avg packet size: %.1f bytes (PaddingMax=24)", audioAvg)
	t.Logf("video avg packet size: %.1f bytes (PaddingMax=60)", videoAvg)

	if videoAvg <= audioAvg {
		t.Errorf("video packets should be larger on average than audio, got video=%.1f <= audio=%.1f", videoAvg, audioAvg)
	}
}

func TestObfsPayloadType_ByteInWire(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	payload := make([]byte, 64)
	rand.Read(payload)

	audioCfg := core.NewObfsConfig("audio")
	videoCfg := core.NewObfsConfig("video")

	// Фиксируем SSRC чтобы сравнение было чистым
	audioCfg.SSRC = 0x12345678
	videoCfg.SSRC = 0x12345678

	audioState := core.NewObfsState()
	videoState := core.NewObfsState()

	aw, err := core.ObfsWrapPacket(key, payload, audioCfg, audioState)
	if err != nil {
		t.Fatal(err)
	}
	vw, err := core.ObfsWrapPacket(key, payload, videoCfg, videoState)
	if err != nil {
		t.Fatal(err)
	}

	// byte[1] = PayloadType
	audioPT := aw[1] & 0x7F
	videoPT := vw[1] & 0x7F

	if audioPT != 111 {
		t.Errorf("audio wire PayloadType: got %d, want 111", audioPT)
	}
	if videoPT != 96 {
		t.Errorf("video wire PayloadType: got %d, want 96", videoPT)
	}

	// Пакеты должны различаться
	if bytes.Equal(aw, vw) {
		t.Error("audio and video packets are identical — modes are not differentiated")
	}
}

// ============================================================
// obfsIsRTPPacket — детектирует оба типа пакетов
// ============================================================

func TestIsRTPPacket_DetectsBothModes(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	payload := make([]byte, 64)
	rand.Read(payload)

	for _, mode := range []string{"audio", "video"} {
		cfg := core.NewObfsConfig(mode)
		state := core.NewObfsState()
		wire, err := core.ObfsWrapPacket(key, payload, cfg, state)
		if err != nil {
			t.Fatal(err)
		}
		if !core.ObfsIsRTPPacket(wire) {
			t.Errorf("mode %s: ObfsIsRTPPacket returned false for valid wrapped packet", mode)
		}
	}
}

func TestIsRTPPacket_RejectsNonRTP(t *testing.T) {
	// Слишком короткий пакет
	if core.ObfsIsRTPPacket([]byte{0x80, 0x6F}) {
		t.Error("should reject short packet")
	}

	// Не RTP version 2
	nonRTP := make([]byte, 20)
	nonRTP[0] = 0x00
	if core.ObfsIsRTPPacket(nonRTP) {
		t.Error("should reject non-RTP version")
	}

	// Random data
	random := make([]byte, 100)
	rand.Read(random)
	if core.ObfsIsRTPPacket(random) {
		t.Error("should reject random data")
	}
}

// ============================================================
// PaddingMax влияет на реальную разброс размеров пакетов
// ============================================================

func TestObfsPaddingRange_Audio(t *testing.T) {
	testPaddingRange(t, "audio", 24, 200)
}

func TestObfsPaddingRange_Video(t *testing.T) {
	testPaddingRange(t, "video", 60, 200)
}

func testPaddingRange(t *testing.T, mode string, expectedMaxPad int, trials int) {
	t.Helper()

	key := make([]byte, 32)
	rand.Read(key)
	payload := make([]byte, 100)
	rand.Read(payload)

	cfg := core.NewObfsConfig(mode)
	state := core.NewObfsState()

	minSize := -1
	maxSize := -1

	for i := 0; i < trials; i++ {
		wire, err := core.ObfsWrapPacket(key, payload, cfg, state)
		if err != nil {
			t.Fatal(err)
		}
		sz := len(wire)
		if minSize < 0 || sz < minSize {
			minSize = sz
		}
		if maxSize < 0 || sz > maxSize {
			maxSize = sz
		}
	}

	expectedMinPayload := 12 + len(payload) + 16 + 1 // header + payload + chacha overhead + padTotal=1
	expectedMaxPayload := 12 + len(payload) + 16 + expectedMaxPad + 1

	t.Logf("%s: min=%d, max=%d, range=%d (expected max pad=%d)", mode, minSize, maxSize, maxSize-minSize, expectedMaxPad)

	if minSize != expectedMinPayload {
		t.Errorf("%s min size: got %d, want %d", mode, minSize, expectedMinPayload)
	}
	if maxSize > expectedMaxPayload+1 {
		t.Errorf("%s max size %d exceeds expected bound %d", mode, maxSize, expectedMaxPayload)
	}
}

// ============================================================
// SSRC в wire совпадает с конфигом
// ============================================================

func TestObfsSSRC_InWire(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	payload := make([]byte, 50)
	rand.Read(payload)

	cfg := core.NewObfsConfig("audio")
	cfg.SSRC = 0xDEADBEEF
	state := core.NewObfsState()

	wire, err := core.ObfsWrapPacket(key, payload, cfg, state)
	if err != nil {
		t.Fatal(err)
	}

	gotSSRC := binary.BigEndian.Uint32(wire[8:12])
	if gotSSRC != 0xDEADBEEF {
		t.Errorf("SSRC in wire: got 0x%08X, want 0xDEADBEEF", gotSSRC)
	}
}

// ============================================================
// Seq и TS инкрементируются
// ============================================================

func TestObfsSeqIncrement(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	payload := make([]byte, 64)
	rand.Read(payload)

	cfg := core.NewObfsConfig("audio")
	state := core.NewObfsState()

	var seqs []uint16
	for i := 0; i < 10; i++ {
		wire, err := core.ObfsWrapPacket(key, payload, cfg, state)
		if err != nil {
			t.Fatal(err)
		}
		seq := binary.BigEndian.Uint16(wire[2:4])
		seqs = append(seqs, seq)
	}

	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("seq not monotonically increasing: seq[%d]=%d >= seq[%d]=%d", i, seqs[i], i-1, seqs[i-1])
		}
	}
}
