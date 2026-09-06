package locallogin

import (
	"errors"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"testing"
)

func TestYU32HShortPasswordEnrollmentRejected(t *testing.T) {
	f := newLoginFixture(t, false)
	_, err := f.credentials.SetPassword(t.Context(), "org-a", "user-a", []byte("short"), 1)
	if !errors.Is(err, localcredential.ErrInvalidPassword) {
		t.Fatal("YU32H_RED: short password enrollment was accepted")
	}
	metadata, err := f.credentials.Metadata(t.Context(), "org-a", "user-a")
	if err != nil || metadata.Revision != 1 {
		t.Fatal("weak enrollment changed credential revision")
	}
}

func TestYU32HOnlineGuessingIsBounded(t *testing.T) {
	f := newLoginFixture(t, false)
	input := LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("incorrect-password")}
	for i := 0; i < 10; i++ {
		if _, err := f.manager.Login(f.context(t), input); !errors.Is(err, ErrAuthenticationFailed) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if _, err := f.manager.Login(f.context(t), input); err == nil || errors.Is(err, ErrAuthenticationFailed) {
		t.Fatal("YU32H_RED: password guesses remain unbounded after 11 attempts")
	}
	if f.sessionCount(t) != 0 {
		t.Fatal("failed/limited login created a session")
	}
}
