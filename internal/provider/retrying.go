package provider

import (
	"context"
	"errors"
	"math"
	"time"

	"golang.org/x/exp/slog"
)

// retryingProvider 是 Provider Manager 创建的唯一重试 owner decorator。
// timeout 是一次逻辑调用的总 deadline；maxRetries 不含首次请求；retryInterval 是首次退避基数。
type retryingProvider struct {
	inner         Provider
	timeout       time.Duration
	maxRetries    int
	retryInterval time.Duration
	logger        *slog.Logger
}

func newRetrying(inner Provider, timeout time.Duration, maxRetries int, retryInterval time.Duration) *retryingProvider {
	if timeout < 0 {
		timeout = 0
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	l := slog.Default()
	return &retryingProvider{
		inner:         inner,
		timeout:       timeout,
		maxRetries:    maxRetries,
		retryInterval: retryInterval,
		logger:        l,
	}
}

func (r *retryingProvider) ID() string          { return r.inner.ID() }
func (r *retryingProvider) Type() string        { return r.inner.Type() }
func (r *retryingProvider) Models() []ModelInfo { return r.inner.Models() }
func (r *retryingProvider) EstimateInputTokens(ctx context.Context, req *ChatRequest) (int, error) {
	return r.inner.EstimateInputTokens(ctx, req)
}
func (r *retryingProvider) Close() error { return r.inner.Close() }

// callContext 派生一次逻辑调用的总 deadline context；caller 已有更早 deadline 自然优先。
func (r *retryingProvider) callContext(parent context.Context) (context.Context, context.CancelFunc) {
	if r.timeout > 0 {
		return context.WithTimeout(parent, r.timeout)
	}
	return context.WithCancel(parent)
}

// shouldRetry 仅当 ProviderError.Retryable 且 ctx 仍有效。
func shouldRetry(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	var pe *ProviderError
	if !errors.As(err, &pe) {
		return false
	}
	if !pe.Retryable {
		return false
	}
	return ctx.Err() == nil
}

// backoff 计算第 retryIndex 次重试的本地退避，并取与 RetryAfter 的较大值。
func backoff(retryIndex int, base, retryAfter time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	shift := retryIndex
	if shift > 30 { // 防溢出。
		shift = 30
	}
	d := base * time.Duration(int64(math.Pow(2, float64(shift))))
	if d > retryBackoffCap {
		d = retryBackoffCap
	}
	if retryAfter > 0 && retryAfter > d {
		d = retryAfter
	}
	return d
}

func sleepRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		delay = 0
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Chat 在收到完整 ChatResponse 前可重试；响应只向调用方返回一次。
func (r *retryingProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	callCtx, cancel := r.callContext(ctx)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		resp, err := r.inner.Chat(callCtx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !shouldRetry(callCtx, err) {
			return nil, err
		}
		if attempt == r.maxRetries {
			break
		}
		var pe *ProviderError
		retryAfter := time.Duration(0)
		if errors.As(err, &pe) {
			retryAfter = pe.RetryAfter
		}
		if sleepErr := sleepRetry(callCtx, backoff(attempt, r.retryInterval, retryAfter)); sleepErr != nil {
			break // ctx 已取消，返回 lastErr（分类为 timeout 等）。
		}
	}
	return nil, lastErr
}

// StreamChat 先缓冲到首个可见 chunk；首个 chunk 前可重试，之后不再重试。
func (r *retryingProvider) StreamChat(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error) {
	callCtx, cancel := r.callContext(ctx)
	out := make(chan ChatChunk, 16)
	go func() {
		defer func() {
			cancel()
			close(out)
		}()
		for attempt := 0; attempt <= r.maxRetries; attempt++ {
			inch, err := r.inner.StreamChat(callCtx, req)
			if err != nil {
				if shouldRetry(callCtx, err) && attempt < r.maxRetries {
					if sleepErr := sleepRetry(callCtx, backoff(attempt, r.retryInterval, retryAfterOf(err))); sleepErr != nil {
						out <- ChatChunk{Error: finalErr(err)}
						return
					}
					continue
				}
				out <- ChatChunk{Error: err}
				return
			}
			// 消费 attempt，直到首个可见 chunk 或结束。
			seenFirst := false
			for {
				select {
				case <-callCtx.Done():
					drainChannel(inch)
					return
				default:
				}
				chunk, ok := <-inch
				if !ok {
					// attempt 干净结束（无更多 chunk）。
					if !seenFirst {
						return // 空流也是正常结束。
					}
					return
				}
				if chunk.Error != nil {
					if !seenFirst && shouldRetry(callCtx, chunk.Error) && attempt < r.maxRetries {
						drainChannel(inch)
						if sleepErr := sleepRetry(callCtx, backoff(attempt, r.retryInterval, retryAfterOf(chunk.Error))); sleepErr != nil {
							out <- ChatChunk{Error: finalErr(chunk.Error)}
							return
						}
						break // 进入下一次 attempt。
					}
					out <- chunk
					return
				}
				seenFirst = true
				select {
				case out <- chunk:
				case <-callCtx.Done():
					drainChannel(inch)
					return
				}
			}
		}
	}()
	return out, nil
}

func retryAfterOf(err error) time.Duration {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.RetryAfter
	}
	return 0
}

// finalErr 在 ctx 取消时返回超时分类，否则原样。
func finalErr(err error) error {
	if err == nil {
		return nil
	}
	var pe *ProviderError
	if errors.As(err, &pe) && pe.Retryable {
		// 取消导致中断：返回 timeout 分类便于上层稳定映射。
		return &ProviderError{Code: ErrCodeTimeout, Message: "stream deadline exceeded", ProviderID: pe.ProviderID, Cause: err}
	}
	return err
}

// drainChannel 消费剩余 chunk，避免 adapter goroutine 阻塞。
func drainChannel(ch <-chan ChatChunk) {
	for range ch {
	}
}

var _ Provider = (*retryingProvider)(nil)
