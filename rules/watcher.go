package rules

import (
	"context"
	"strings"
	"time"

	"github.com/coreos/etcd/clientv3"
	"go.uber.org/zap"
)

func newV3Watcher(ec *clientv3.Client, prefix string, logger *zap.Logger, proc keyProc, watchTimeout int, kvWrapper WrapKV) (watcher, error) {
	api := etcdV3ReadAPI{
		baseReadAPI: baseReadAPI{},
		kV:          kvWrapper(ec),
	}

	ew := newEtcdV3KeyWatcher(clientv3.NewWatcher(ec), prefix, time.Duration(watchTimeout)*time.Second)
	return watcher{
		kv:     kvWrapper(ec),
		api:    &api,
		kw:     ew,
		kp:     proc,
		logger: logger,
		prefix: prefix,
	}, nil
}

type watcher struct {
	api      readAPI
	kv       clientv3.KV
	kw       keyWatcher
	kp       keyProc
	logger   *zap.Logger
	prefix   string
	stopping uint32
	stopped  uint32
}

func (w *watcher) run() {
	atomicSet(&w.stopped, false)
	for !is(&w.stopping) {
		w.singleRun()
	}
	atomicSet(&w.stopped, true)
}

func (w *watcher) stop() {
	atomicSet(&w.stopping, true)
	w.kw.cancel()
}

func (w *watcher) isStopped() bool {
	return is(&w.stopped)
}

func (w *watcher) isStopping() bool {
	return is(&w.stopping)
}

func (w *watcher) singleRun() {
	key, value, err := w.kw.next()
	if err != nil {
		w.logger.Error("Watcher error", zap.Error(err))
		if strings.Contains(err.Error(), "connection refused") {
			w.logger.Info("Cluster unavailable; waiting one minute to retry")
			time.Sleep(time.Minute)
			// make sure we process any changes the watcher might have missed
			w.catchUp()
		} else {
			// Maximum logging rate is 1 per second.
			time.Sleep(time.Second)
		}
		return
	}
	w.kp.processKey(key, value, w.api, w.logger, map[string]string{})
}

func (w *watcher) catchUp() {
	if w.isStopping() {
		return
	}
	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(1)*time.Minute)
	defer cancelFunc()
	ctx = SetMethod(ctx, "watcher-retry")
	values := map[string]string{}
	resp, err := w.kv.Get(ctx, w.prefix, clientv3.WithPrefix())
	if err != nil {
		w.logger.Error("Error retrieving prefix", zap.Error(err))
		return
	}
	for _, kv := range resp.Kvs {
		values[string(kv.Key)] = string(kv.Value)
	}
	w.processData(values)
}

func (w *watcher) processData(values map[string]string) {
	api := &cacheReadAPI{values: values}
	for k, v := range values {
		if w.isStopping() {
			return
		}
		// Check to see if any rule is satisfied from cache
		if w.kp.isWork(k, &v, api) {
			// Process key if it is
			w.kp.processKey(k, &v, w.api, w.logger, map[string]string{"source": "watcher-retry"})
		}
		time.Sleep(time.Duration(ic.delay) * time.Millisecond)
	}
}