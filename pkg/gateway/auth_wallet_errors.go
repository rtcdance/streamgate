package gateway

import (
	"errors"
	"net/http"

	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/web3/signature"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func walletAuthError(err error) (httpStatus int, code, message string) {
	switch {
	case errors.Is(err, signature.ErrWalletDependency), errors.Is(err, service.ErrRestoreEpochUnreadable):
		return http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "wallet verification unavailable"
	case errors.Is(err, signature.ErrWalletUnsupported), errors.Is(err, service.ErrNotSupported):
		return http.StatusUnprocessableEntity, "WALLET_UNSUPPORTED", "wallet or signing scheme is not supported"
	case errors.Is(err, signature.ErrWalletFormat), errors.Is(err, service.ErrInvalidRequest), errors.Is(err, service.ErrChainIDMismatch):
		return http.StatusBadRequest, ErrInvalidRequest, "invalid wallet authentication request"
	case errors.Is(err, service.ErrTokenExpired), errors.Is(err, service.ErrChallengeExpired):
		return http.StatusUnauthorized, ErrTokenExpired, "authentication expired"
	default:
		return http.StatusUnauthorized, ErrUnauthorized, "authentication failed"
	}
}

func walletAuthGRPCError(err error) error {
	httpStatus, _, message := walletAuthError(err)
	switch httpStatus {
	case http.StatusServiceUnavailable:
		return status.Error(codes.Unavailable, message)
	case http.StatusBadRequest:
		return status.Error(codes.InvalidArgument, message)
	case http.StatusUnprocessableEntity:
		return status.Error(codes.Unimplemented, message)
	default:
		return status.Error(codes.Unauthenticated, message)
	}
}

func walletCapabilityError(err error) (httpStatus int, code, message string) {
	switch {
	case errors.Is(err, service.ErrPlaybackDenied):
		return http.StatusForbidden, ErrNFTRequired, "nft access denied"
	case errors.Is(err, service.ErrAuthorizationSessionRequired), errors.Is(err, service.ErrInvalidCredential), errors.Is(err, service.ErrTokenExpired):
		return http.StatusUnauthorized, ErrUnauthorized, "authentication required"
	case errors.Is(err, signature.ErrWalletDependency), errors.Is(err, service.ErrAuthorizationUnavailable), errors.Is(err, service.ErrRestoreEpochUnreadable):
		return http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "verification unavailable"
	default:
		return http.StatusInternalServerError, ErrInternalError, "capability issuance failed"
	}
}
