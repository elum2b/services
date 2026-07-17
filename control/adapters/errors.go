package auth

import serviceerrors "github.com/elum2b/services/errors"

var (
	ErrAdminRequired             = serviceerrors.New(serviceerrors.CodeInvalidFields, "control admin service is required")
	ErrProviderRequired          = serviceerrors.New(serviceerrors.CodeInvalidFields, "auth provider is required")
	ErrProviderExists            = serviceerrors.New(serviceerrors.CodeConflict, "auth provider already exists")
	ErrProviderNotFound          = serviceerrors.New(serviceerrors.CodeNotFound, "auth provider not found")
	ErrSubjectRequired           = serviceerrors.New(serviceerrors.CodeUnauthorized, "auth provider subject is required")
	ErrTokenRequired             = serviceerrors.New(serviceerrors.CodeInvalidFields, "auth token is required")
	ErrCodeRequired              = serviceerrors.New(serviceerrors.CodeInvalidFields, "auth code is required")
	ErrEndpointRequired          = serviceerrors.New(serviceerrors.CodeInvalidFields, "auth provider endpoint is required")
	ErrClientIDRequired          = serviceerrors.New(serviceerrors.CodeInvalidFields, "auth provider client id is required")
	ErrClientSecretRequired      = serviceerrors.New(serviceerrors.CodeInvalidFields, "auth provider client secret is required")
	ErrBotTokenRequired          = serviceerrors.New(serviceerrors.CodeInvalidFields, "telegram bot token is required")
	ErrInvalidSignature          = serviceerrors.New(serviceerrors.CodeUnauthorized, "auth provider signature is invalid")
	ErrAuthDataExpired           = serviceerrors.New(serviceerrors.CodeUnauthorized, "auth provider data is expired")
	ErrPayloadRequired           = serviceerrors.New(serviceerrors.CodeInvalidFields, "auth payload is required")
	ErrAddressRequired           = serviceerrors.New(serviceerrors.CodeInvalidFields, "wallet address is required")
	ErrDomainRequired            = serviceerrors.New(serviceerrors.CodeInvalidFields, "auth domain is required")
	ErrInvalidDomain             = serviceerrors.New(serviceerrors.CodeUnauthorized, "auth domain is invalid")
	ErrInvalidNetwork            = serviceerrors.New(serviceerrors.CodeUnauthorized, "wallet network is invalid")
	ErrStateInitOrClientRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "wallet state init or ton client is required")
)
