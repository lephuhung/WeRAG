package service

import (
	"context"
	"errors"
	"strings"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ErrTerminalAuthDenied is a definite authorization failure for an open
// sandbox terminal: the minting access token is gone or revoked, the user
// is inactive or deleted, membership was removed, or the session is no
// longer owned.
//
// Every other error is a lookup failure and is returned as-is. The
// distinction matters: this runs on a timer against every open terminal, so
// treating a database hiccup as "denied" would disconnect the whole fleet at
// once and then have it all reconnect together. The middleware takes the
// same stance (see resolveTenantRole's membership lookup).
var ErrTerminalAuthDenied = errors.New("terminal authorization no longer valid")

// CheckSandboxTerminalAuth re-validates the identity that opened a PTY and
// returns the resolved user, so the handshake can install the auth session
// without looking the user up a second time.
//
// Call it before the upgrade and periodically on the live bridge; using the
// one function for both is deliberate, so a live terminal is held to exactly
// the checks that admitted it — except token expiry. Handshake minting and
// the upgrade still require a non-expired access token; the live recheck
// does not, because axios silently rotates access tokens without revoking
// the row the ticket was bound to. Logout / password reset still revoke
// every row and tear the PTY down.
//
// rejectExpired is true for the handshake and false for the open-PTY timer.
func CheckSandboxTerminalAuth(
	ctx context.Context,
	users interfaces.UserService,
	members interfaces.TenantMemberService,
	sessions interfaces.SessionService,
	claims SandboxTerminalTicketClaims,
	rejectExpired bool,
) (*types.User, error) {
	if users == nil || members == nil || sessions == nil {
		return nil, ErrTerminalAuthDenied
	}
	userID := strings.TrimSpace(claims.UserID)
	sessionID := strings.TrimSpace(claims.SessionID)
	tokenID := strings.TrimSpace(claims.TokenID)
	if userID == "" || sessionID == "" || tokenID == "" || claims.TenantID == 0 {
		return nil, ErrTerminalAuthDenied
	}

	token, err := users.GetAccessTokenByID(ctx, tokenID)
	if err != nil {
		if errors.Is(err, apprepo.ErrTokenNotFound) {
			return nil, ErrTerminalAuthDenied
		}
		return nil, err
	}
	if token == nil {
		return nil, ErrTerminalAuthDenied
	}
	if err := AssertAccessTokenNotRevoked(token, userID); err != nil {
		return nil, err
	}
	if rejectExpired {
		if err := assertAccessTokenNotExpired(token, time.Now()); err != nil {
			return nil, err
		}
	}

	user, err := users.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, apprepo.ErrUserNotFound) {
			return nil, ErrTerminalAuthDenied
		}
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, ErrTerminalAuthDenied
	}

	if err := terminalMembershipStillValid(ctx, members, user, claims.TenantID); err != nil {
		return nil, err
	}

	ownedCtx := sandboxTerminalAuthContext(ctx, userID, claims.TenantID)
	if _, err := sessions.GetOwnedSession(ownedCtx, sessionID); err != nil {
		if errors.Is(err, apperrors.ErrSessionNotFound) {
			return nil, ErrTerminalAuthDenied
		}
		return nil, err
	}
	return user, nil
}

// AssertAccessTokenNotRevoked reports whether a stored access-token row is
// still the minting credential for userID (right owner and type, not
// revoked). Expiry is checked separately: a live PTY must survive silent
// access-token rotation, while minting a new ticket still requires a
// current token.
func AssertAccessTokenNotRevoked(token *types.AuthToken, userID string) error {
	if token == nil {
		return ErrTerminalAuthDenied
	}
	if strings.TrimSpace(token.UserID) != userID {
		return ErrTerminalAuthDenied
	}
	if token.TokenType != "access_token" {
		return ErrTerminalAuthDenied
	}
	if token.IsRevoked {
		return ErrTerminalAuthDenied
	}
	return nil
}

func assertAccessTokenNotExpired(token *types.AuthToken, now time.Time) error {
	if token == nil {
		return ErrTerminalAuthDenied
	}
	if !token.ExpiresAt.IsZero() && !token.ExpiresAt.After(now) {
		return ErrTerminalAuthDenied
	}
	return nil
}

// AssertAccessTokenStillActive is the minting check: not revoked and not
// expired. The handshake ticket POST uses this so a stale Bearer token
// cannot open a new PTY.
func AssertAccessTokenStillActive(token *types.AuthToken, userID string, now time.Time) error {
	if err := AssertAccessTokenNotRevoked(token, userID); err != nil {
		return err
	}
	return assertAccessTokenNotExpired(token, now)
}

// terminalMembershipStillValid answers "may this user still act in this
// workspace", the recheck counterpart to middleware.resolveTenantRole and
// the SuperAdmin branch of authenticateJWTUser.
//
// The rule is the same as the middleware's: an active membership, or an
// explicit human SuperAdmin (IsSystemAdmin on a non-synthetic user), who is
// admitted to every tenant without one. The legacy CanAccessAllTenants
// attribute grants nothing, and enable_rbac=false does not fail open:
// identity never depends on the RBAC rollout switch.
func terminalMembershipStillValid(
	ctx context.Context,
	members interfaces.TenantMemberService,
	user *types.User,
	tenantID uint64,
) error {
	member, err := members.GetMembership(ctx, user.ID, tenantID)
	if err != nil {
		return err
	}
	if member != nil && member.Status == types.TenantMemberStatusActive {
		return nil
	}
	if user.IsSystemAdmin && !types.IsSyntheticUserID(user.ID) {
		return nil
	}
	return ErrTerminalAuthDenied
}

func sandboxTerminalAuthContext(parent context.Context, userID string, tenantID uint64) context.Context {
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: userID})
}
