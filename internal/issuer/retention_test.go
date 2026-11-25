package issuer

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func publishedKids(t *testing.T, h http.Handler) []string {
	t.Helper()
	rec := getJWKS(h)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /jwks status = %d, want 200", rec.Code)
	}
	var kids []string
	for _, jwk := range decodeJWKS(t, rec) {
		kids = append(kids, jwk.Kid)
	}
	return kids
}

func wantKids(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("published kids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("published kids = %v, want %v", got, want)
		}
	}
}

func TestOldKeyStaysPublishedWhileItsTokensCanStillBeValid(t *testing.T) {
	mr := seededRedis(t)
	keys := keysAged(t, 72*time.Hour, testDelay+5*time.Minute)
	h := NewHandler(redisClient(t, mr.Addr()), keys, testDelay).Routes()

	if got := signingKid(t, h); got != keys[1].Kid {
		t.Fatalf("signed with %q, want the new key %q", got, keys[1].Kid)
	}

	wantKids(t, publishedKids(t, h), keys[0].Kid, keys[1].Kid)
}

func TestOldKeyIsRemovedOnceItsLastTokenHasExpired(t *testing.T) {
	mr := seededRedis(t)
	keys := keysAged(t, 72*time.Hour, testDelay+TokenExpirationInterval+KeyRemovalErrorMargin+time.Minute)
	h := NewHandler(redisClient(t, mr.Addr()), keys, testDelay).Routes()

	signingKid(t, h)

	wantKids(t, publishedKids(t, h), keys[1].Kid)
}

func TestRemovalKeepsAKeyThatIsPublishedButNotYetSigning(t *testing.T) {
	mr := seededRedis(t)
	keys := keysAged(t, 72*time.Hour, 3*time.Hour, 5*time.Minute)
	h := NewHandler(redisClient(t, mr.Addr()), keys, testDelay).Routes()

	if got := signingKid(t, h); got != keys[1].Kid {
		t.Fatalf("signed with %q, want key 1 %q", got, keys[1].Kid)
	}

	wantKids(t, publishedKids(t, h), keys[1].Kid, keys[2].Kid)
}

func TestNothingIsRemovedWithoutANewerKeyAlreadySigning(t *testing.T) {
	mr := seededRedis(t)
	keys := keysAged(t, 72*time.Hour, 10*time.Minute)
	h := NewHandler(redisClient(t, mr.Addr()), keys, testDelay).Routes()

	signingKid(t, h)

	wantKids(t, publishedKids(t, h), keys[0].Kid, keys[1].Kid)
}

func TestHandlerDoesNotModifyTheSliceItWasGiven(t *testing.T) {
	mr := seededRedis(t)
	keys := keysAged(t, 72*time.Hour, 3*time.Hour, 5*time.Minute)
	before := make([]string, len(keys))
	for i, key := range keys {
		before[i] = key.Kid
	}
	h := NewHandler(redisClient(t, mr.Addr()), keys, testDelay).Routes()

	signingKid(t, h)

	for i, key := range keys {
		if key.Kid != before[i] || key.Key == nil {
			t.Fatalf("caller's slice changed at %d: kid %q, want %q", i, key.Kid, before[i])
		}
	}
}

func TestConcurrentTokenAndJWKSRequestsDuringARemoval(t *testing.T) {
	mr := seededRedis(t)
	keys := keysAged(t, 72*time.Hour, 3*time.Hour, 5*time.Minute)
	h := NewHandler(redisClient(t, mr.Addr()), keys, testDelay).Routes()

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if rec := postToken(h, testAPIKey); rec.Code != http.StatusCreated {
				t.Errorf("POST /token status = %d, want 201", rec.Code)
			}
		}()
		go func() {
			defer wg.Done()
			if rec := getJWKS(h); rec.Code != http.StatusOK {
				t.Errorf("GET /jwks status = %d, want 200", rec.Code)
			}
		}()
	}
	wg.Wait()

	wantKids(t, publishedKids(t, h), keys[1].Kid, keys[2].Kid)
}
