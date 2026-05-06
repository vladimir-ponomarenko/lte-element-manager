package pm

import "testing"

func TestIsAllowedCanonicalKeyIncludesTCAInputs(t *testing.T) {
	keys := []string{
		CanonicalS1APReady,
		CanonicalNASDLDrop,
		CanonicalNASULFail,
		CanonicalNASULSecUnknown,
		CanonicalNASULParseFail,
		CanonicalNASDLParseFail,
		CanonicalNASDLServiceRej,
		CanonicalRRCProtocolFail,
		CanonicalRRCConRejectTX,
		CanonicalRRCPagingFail,
		CanonicalRRCMaxRLCRetx,
		CanonicalUEULPUCCHNI,
		CanonicalUEULPHR,
		CanonicalUERRCRLFCnt,
		CanonicalUERRCInactivity,
		"bearer.9.dl_buffered_bytes",
	}
	for _, key := range keys {
		if !IsAllowedCanonicalKey(key) {
			t.Fatalf("expected %s to be allowed", key)
		}
	}
	if IsAllowedCanonicalKey("bearer.9.unknown") {
		t.Fatalf("unexpected dynamic bearer key allowed")
	}
}
