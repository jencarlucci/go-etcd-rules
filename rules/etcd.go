package rules

import (
	"errors"
	"sync"
	"time"

	"github.com/coreos/etcd/clientv3"
	"golang.org/x/net/context"
	"github.com/coreos/etcd/mvcc/mvccpb"
)

type baseReadAPI struct {
	cancelFunc context.CancelFunc
}

func (bra *baseReadAPI) getContext() context.Context {
	var ctx context.Context
	ctx, bra.cancelFunc = context.WithTimeout(context.Background(), time.Duration(60)*time.Second)
	ctx = SetMethod(ctx, "rule_eval")
	return ctx
}

func (bra *baseReadAPI) cancel() {
	bra.cancelFunc()
}

type etcdV3ReadAPI struct {
	baseReadAPI
	kV clientv3.KV
}

func (edv3ra *etcdV3ReadAPI) get(key string) (*string, error) {
	ctx := edv3ra.baseReadAPI.getContext()
	defer edv3ra.cancel()
	resp, err := edv3ra.kV.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if resp.Count == 0 {
		return nil, nil
	}
	val := string(resp.Kvs[0].Value)
	return &val, nil
}

func (edv3ra *etcdV3ReadAPI) getAll(key string) ([]*mvccpb.KeyValue, error) {
	ctx := edv3ra.baseReadAPI.getContext()
	defer edv3ra.cancel()
	resp, err := edv3ra.kV.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if resp.Count == 0 {
		return nil, nil
	}
	return resp.Kvs, nil
}

type keyWatcher interface {
	next() (string, *string, error)
	cancel()
}

func newEtcdV3KeyWatcher(watcher clientv3.Watcher, prefix string, timeout time.Duration) *etcdV3KeyWatcher {
	_, cancel := context.WithCancel(context.Background())
	kw := etcdV3KeyWatcher{
		baseKeyWatcher: baseKeyWatcher{
			prefix:     prefix,
			timeout:    timeout,
			cancelFunc: cancel,
		},
		w:      watcher,
		stopCh: make(chan bool),
	}
	return &kw
}

type baseKeyWatcher struct {
	cancelFunc  context.CancelFunc
	cancelMutex sync.Mutex
	prefix      string
	timeout     time.Duration
	stopping    uint32
}

func (bkw *baseKeyWatcher) getContext() context.Context {
	ctx := context.Background()
	bkw.cancelMutex.Lock()
	defer bkw.cancelMutex.Unlock()
	if bkw.timeout > 0 {
		ctx, bkw.cancelFunc = context.WithTimeout(ctx, bkw.timeout)
	} else {
		ctx, bkw.cancelFunc = context.WithCancel(ctx)
	}
	ctx = SetMethod(ctx, "watcher")
	return ctx
}

func (bkw *baseKeyWatcher) cancel() {
	atomicSet(&bkw.stopping, true)
	bkw.cancelMutex.Lock()
	defer bkw.cancelMutex.Unlock()
	if bkw.cancelFunc != nil {
		bkw.cancelFunc()
		bkw.cancelFunc = nil
	}
}

type etcdV3KeyWatcher struct {
	baseKeyWatcher
	ch     clientv3.WatchChan
	stopCh chan bool
	events []*clientv3.Event
	w      clientv3.Watcher
}

func (ev3kw *etcdV3KeyWatcher) cancel() {
	ev3kw.stopCh <- true
}

func (ev3kw *etcdV3KeyWatcher) next() (string, *string, error) {
	if ev3kw.ch == nil {
		ev3kw.ch = ev3kw.w.Watch(context.Background(), ev3kw.prefix, clientv3.WithPrefix())
	}
	if ev3kw.events == nil || len(ev3kw.events) == 0 {
		select {
		case <-ev3kw.stopCh:
			ev3kw.cancelFunc()
			err := ev3kw.w.Close()
			if err == nil {
				err = errors.New("Watcher closing")
			}
			return "", nil, err
		case wr, stillOpen := <-ev3kw.ch:
			var err error
			// Check if channel is closed without
			// an event with an error having been
			// added.
			if !stillOpen {
				err = errors.New("Channel closed")
			}
			// If there is an error, the logic appears to always
			// close the channel, so there is no need to try to
			// close it here.
			if err == nil {
				err = wr.Err()
			}
			if err != nil {
				ev3kw.ch = nil
				return "", nil, err
			}
			ev3kw.events = wr.Events
		}
	}
	if ev3kw.events == nil || len(ev3kw.events) == 0 {
		ev3kw.ch = nil
		return "", nil, errors.New("No events received from watcher channel; instantiating new channel")
	}
	event := ev3kw.events[0]
	ev3kw.events = ev3kw.events[1:]
	key := string(event.Kv.Key)
	if event.Type == clientv3.EventTypeDelete { // Covers lease expiration
		return key, nil, nil
	}
	val := string(event.Kv.Value)
	return key, &val, nil
}
