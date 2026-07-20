package microvm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	ackmetrics "github.com/aws-controllers-k8s/runtime/pkg/metrics"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	svcsdk "github.com/aws/aws-sdk-go-v2/service/lambdamicrovms"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	svcapitypes "github.com/aws-controllers-k8s/lambdamicrovms-controller/apis/v1alpha1"
)

func TestSDKCreateSendsAnnotatedClientToken(t *testing.T) {
	wantToken := strings.Repeat("c", 64)
	receivedTokens := make([]string, 0, 2)
	receivedPayloads := make([]map[string]any, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("request method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/2025-09-09/microvms" {
			t.Errorf("request path = %s, want /2025-09-09/microvms", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode RunMicrovm request: %v", err)
		}
		token, _ := payload["clientToken"].(string)
		receivedTokens = append(receivedTokens, token)
		receivedPayloads = append(receivedPayloads, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"endpoint":"https://vm.example",
			"imageArn":"arn:aws:lambda:us-west-2:123456789012:microvm-image:test",
			"imageVersion":"1",
			"maximumDurationInSeconds":900,
			"microvmId":"vm-test",
			"startedAt":1,
			"state":"RUNNING"
		}`))
	}))
	t.Cleanup(server.Close)

	cfg := aws.Config{
		Region: "us-west-2",
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			"test-access-key", "test-secret-key", "",
		)),
		HTTPClient:   server.Client(),
		BaseEndpoint: aws.String(server.URL),
	}
	rm := &resourceManager{
		sdkapi:  svcsdk.NewFromConfig(cfg),
		metrics: ackmetrics.NewMetrics("lambda-microvms-test"),
	}
	desired := &resource{ko: &svcapitypes.Microvm{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{clientTokenAnnotation: wantToken},
		},
		Spec: svcapitypes.MicrovmSpec{
			ImageIdentifier:          aws.String("arn:aws:lambda:us-west-2:123456789012:microvm-image:test"),
			MaximumDurationInSeconds: aws.Int64(900),
		},
	}}

	for attempt := 0; attempt < 2; attempt++ {
		created, err := rm.sdkCreate(context.Background(), desired)
		if err != nil {
			t.Fatalf("sdkCreate attempt %d: %v", attempt+1, err)
		}
		if got := aws.ToString(created.ko.Status.MicrovmID); got != "vm-test" {
			t.Fatalf("created MicrovmID = %q, want vm-test", got)
		}
		if created.ko.Spec.ImageVersion != nil {
			t.Fatalf("created spec imageVersion = %q, want original nil value", aws.ToString(created.ko.Spec.ImageVersion))
		}
		// Model a controller crash after AWS accepted the request: status is
		// unavailable, so ACK enters create again with the persisted CR spec.
		desired = &resource{ko: created.ko.DeepCopy()}
		desired.ko.Status = svcapitypes.MicrovmStatus{}
	}
	if len(receivedTokens) != 2 {
		t.Fatalf("RunMicrovm request count = %d, want 2", len(receivedTokens))
	}
	for attempt, got := range receivedTokens {
		if got != wantToken {
			t.Fatalf("RunMicrovm attempt %d clientToken = %q, want %q", attempt+1, got, wantToken)
		}
	}
	if !reflect.DeepEqual(receivedPayloads[0], receivedPayloads[1]) {
		t.Fatalf("RunMicrovm retry payload changed:\nfirst:  %#v\nsecond: %#v", receivedPayloads[0], receivedPayloads[1])
	}
}

func TestClientTokenFor(t *testing.T) {
	valid := strings.Repeat("a", 64)
	cases := []struct {
		name      string
		resource  *resource
		want      string
		wantError string
	}{
		{
			name:      "nil resource",
			wantError: "nil Microvm resource",
		},
		{
			name:      "missing annotation",
			resource:  &resource{ko: &svcapitypes.Microvm{}},
			wantError: "required annotation",
		},
		{
			name: "token too long",
			resource: &resource{ko: &svcapitypes.Microvm{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{clientTokenAnnotation: strings.Repeat("b", 129)},
			}}},
			wantError: "exceeds the AWS maximum",
		},
		{
			name: "valid token",
			resource: &resource{ko: &svcapitypes.Microvm{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{clientTokenAnnotation: valid},
			}}},
			want: valid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := clientTokenFor(tc.resource)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("clientTokenFor() error = %v, want substring %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("clientTokenFor() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("clientTokenFor() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsDeleting_NilTimestamp(t *testing.T) {
	rm := &resourceManager{}
	r := &resource{ko: &svcapitypes.Microvm{}}
	if rm.isDeleting(r) {
		t.Error("expected isDeleting to return false when DeletionTimestamp is nil")
	}
}

func TestIsDeleting_ZeroTimestamp(t *testing.T) {
	rm := &resourceManager{}
	r := &resource{ko: &svcapitypes.Microvm{
		ObjectMeta: metav1.ObjectMeta{
			DeletionTimestamp: &metav1.Time{},
		},
	}}
	if rm.isDeleting(r) {
		t.Error("expected isDeleting to return false when DeletionTimestamp is zero")
	}
}

func TestIsDeleting_SetTimestamp(t *testing.T) {
	rm := &resourceManager{}
	now := metav1.Now()
	r := &resource{ko: &svcapitypes.Microvm{
		ObjectMeta: metav1.ObjectMeta{
			DeletionTimestamp: &now,
		},
	}}
	if !rm.isDeleting(r) {
		t.Error("expected isDeleting to return true when DeletionTimestamp is set")
	}
}

func TestRequeueWaitWhileTerminating(t *testing.T) {
	if requeueWaitWhileTerminating.Duration() != 5*time.Second {
		t.Errorf("expected 5s requeue duration, got %v", requeueWaitWhileTerminating.Duration())
	}
}

// TestIsTerminated guards the delete-unwedge fix: only a Microvm that has
// reached TERMINATED must short-circuit delete (return nil,nil so the runtime
// removes the finalizer), because AWS never returns NotFound for a retained
// terminated VM. Every other state must return false so the controller does not
// drop the finalizer prematurely: TERMINATING is transient and still progresses
// to TERMINATED, and non-terminal states like SUSPENDED may reflect a customer
// actively suspending/resuming the VM — the controller must not interfere.
func TestIsTerminated(t *testing.T) {
	cases := []struct {
		name  string
		state *string
		want  bool
	}{
		{"nil state", nil, false},
		{"running", aws.String(string(svcapitypes.MicrovmState_RUNNING)), false},
		{"suspended", aws.String(string(svcapitypes.MicrovmState_SUSPENDED)), false},
		{"suspending", aws.String(string(svcapitypes.MicrovmState_SUSPENDING)), false},
		{"pending", aws.String(string(svcapitypes.MicrovmState_PENDING)), false},
		{"terminating", aws.String(string(svcapitypes.MicrovmState_TERMINATING)), false},
		{"terminated", aws.String(string(svcapitypes.MicrovmState_TERMINATED)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &resource{ko: &svcapitypes.Microvm{}}
			r.ko.Status.State = tc.state
			if got := isTerminated(r); got != tc.want {
				t.Errorf("isTerminated(state=%v) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}
