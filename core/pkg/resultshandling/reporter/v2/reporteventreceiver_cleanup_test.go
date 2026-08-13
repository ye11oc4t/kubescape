package reporter

import (
	"errors"
	"net/http"
	"testing"

	v1 "github.com/kubescape/backend/pkg/client/v1"
	reporthandlingv2 "github.com/kubescape/opa-utils/reporthandling/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type cleanupErrorTenant struct {
	*TenantConfigMock
	err error
}

func (t *cleanupErrorTenant) DeleteCredentials() error {
	return t.err
}

func TestSendReportPreservesGeneratedAccountCleanupError(t *testing.T) {
	const accountID = "1e3ae7c4-a8bb-4d7c-9bdf-eb86bc25e6bb"
	submitErr := errors.New("injected submission failure")
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, submitErr
	})}
	client, err := v1.NewKSCloudAPI(
		"https://example.com",
		"https://example.com",
		accountID,
		"",
		v1.WithHTTPClient(httpClient),
	)
	require.NoError(t, err)

	cleanupErr := errors.New("injected credential cleanup failure")
	tenant := &cleanupErrorTenant{
		TenantConfigMock: &TenantConfigMock{clusterName: "test", accountID: accountID},
		err:              cleanupErr,
	}
	receiver := NewReportEventReceiver(tenant, "cbabd56f-bac6-416a-836b-b815ef347647", SubmitContextScan, client)
	receiver.accountIdGenerated = true

	err = receiver.sendReport(&reporthandlingv2.PostureReport{}, 0, true)
	require.Error(t, err)
	assert.ErrorIs(t, err, submitErr)
	assert.ErrorIs(t, err, cleanupErr)
	assert.Contains(t, err.Error(), "failed to delete generated credentials")
}
