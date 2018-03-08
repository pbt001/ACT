package fetcher

import (
	"sync"
	"log"
	"sync/atomic"
	"time"
	"github.com/AutomaticCoinTrader/ACT/tools/proxy/zaif/configurator"
	"github.com/AutomaticCoinTrader/ACT/exchange/zaif"
	"path"
	"github.com/AutomaticCoinTrader/ACT/utility"
	"github.com/AutomaticCoinTrader/ACT/tools/proxy/zaif/server"
)

const (
	defaultPollingConcurrency = 4
)

type currencyPairsInfo struct {
	Bids      map[string][][]float64
	Asks      map[string][][]float64
	LastPrice map[string]float64
	mutex     *sync.Mutex
}

func (c *currencyPairsInfo) updateDepth(currencyPair string, currencyPairsBids [][]float64, currencyPairsAsks [][]float64) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.Bids[currencyPair] = currencyPairsBids
	c.Asks[currencyPair] = currencyPairsAsks
}

type Fetcher struct {
	config *configurator.ZaifProxyConfig
	requester *zaif.Requester
	httpClient *utility.HTTPClient
	pollingFinish     int32
	currencyPairsInfo *currencyPairsInfo
	websocketServer   *server.WebsocketServer
}

func  (f *Fetcher) pollingLoop(pollingRequestChan chan string, lastBidsMap map[string][][]float64, lastAsksMap map[string][][]float64, lastBidsAsksMutex *sync.Mutex) {
	log.Printf("start polling loop")
	for {
		currencyPair, ok := <- pollingRequestChan
		if !ok {
			log.Printf("finish polling loop")
			return
		}
		request := f.requester.MakePublicRequest(path.Join("depth", currencyPair), "")
		res, resBody, err := f.httpClient.DoRequest(utility.HTTPMethodGET, request, true)
		if err != nil {
			if res.StatusCode == 403 {
				log.Printf("occured 403 Forbidden currency pair = %v", currencyPair)
			} else {
				log.Printf("can not get depcth (url = %v)", request.URL)
			}
		}
		f.websocketServer.BroadCast(currencyPair, resBody)
	}
}

func  (f *Fetcher) pollingRequestLoop() {
	log.Printf("start polling request loop")
	atomic.StoreInt32(&f.pollingFinish, 0)
	lastBidsMap := make(map[string][][]float64)
	lastAsksMap := make(map[string][][]float64)
	lastBidsAsksMutex := new(sync.Mutex)
	pollingRequestChan := make(chan string)
	for i := 0; i < f.config.PollingConcurrency; i++ {
		go f.pollingLoop(pollingRequestChan, lastBidsMap, lastAsksMap, lastBidsAsksMutex)
	}
FINISH:
	for {
		log.Printf("start get depth of currency Pairs (%v)", time.Now().UnixNano())
		for _, currencyPair := range f.config.CurrencyPairs {
			if atomic.LoadInt32(&f.pollingFinish) == 1{
				break FINISH
			}
			pollingRequestChan <- currencyPair
		}
	}
	close(pollingRequestChan)
	log.Printf("finish polling request loop")
}


func (f *Fetcher) Start() {
	go f.pollingRequestLoop()
	f.websocketServer.Start()
}

func (f *Fetcher) Stop() {
	f.websocketServer.Stop()
	atomic.StoreInt32(&f.pollingFinish, 1)
}

func NewFetcher(config *configurator.ZaifProxyConfig) (*Fetcher) {
	requesterKeys := make([]*zaif.RequesterKey, 0)
	if config.PollingConcurrency == 0 {
		config.PollingConcurrency = defaultPollingConcurrency
	}
	return &Fetcher{
		requester:  zaif.NewRequester(requesterKeys, config.Retry, config.RetryWait, config.Timeout, config.ReadBufSize, config.WriteBufSize),
		httpClient:   utility.NewHTTPClient(config.Retry, config.RetryWait, config.Timeout),
		config: config,
		pollingFinish: 0,
		currencyPairsInfo: &currencyPairsInfo{
			Bids:      make(map[string][][]float64),
			Asks:      make(map[string][][]float64),
			LastPrice: make(map[string]float64),
			mutex:     new(sync.Mutex),
		},
		websocketServer: server.NewWsServer(config),
	}
}