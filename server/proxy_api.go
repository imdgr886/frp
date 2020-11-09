package server

import (
	"encoding/json"
	"io/ioutil"
	"net"
	"net/http"
	"reflect"

	"github.com/fatedier/frp/pkg/consts"
	"github.com/fatedier/frp/pkg/metrics/mem"
	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/server/metrics"

	plugin "github.com/fatedier/frp/pkg/plugin/server"
	"github.com/fatedier/frp/pkg/util/log"
	"github.com/gorilla/mux"
)

func (svr *Service) RunApiServer(addr string) error {
	// url router
	router := mux.NewRouter()

	// auth

	router.HandleFunc("/api/client", svr.ClientList).Methods("GET")
	router.HandleFunc("/api/proxy", svr.AddProxy).Methods("POST")
	router.HandleFunc("/api/proxy/{type}", svr.ProxyByType).Methods("GET")

	s := &http.Server{
		Addr:    addr,
		Handler: router,
	}
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	go s.Serve(ln)
	log.Info("frps restapi listen on tcp %s", ln.Addr().String())
	return nil
}

func (svr *Service) AddProxy(w http.ResponseWriter, r *http.Request) {
	data, _ := ioutil.ReadAll(r.Body)

	proxyParams := &msg.NewProxy{}
	json.Unmarshal(data, proxyParams)

	ctl, online := svr.ctlManager.GetByKey(proxyParams.ClientKey)
	if online {
		content := &plugin.NewProxyContent{
			User: plugin.UserInfo{
				User:  ctl.loginMsg.User,
				Metas: ctl.loginMsg.Metas,
				RunID: ctl.loginMsg.RunID,
			},
			NewProxy: *proxyParams,
		}
		var remoteAddr string
		retContent, err := ctl.pluginManager.NewProxy(content)
		if err == nil {
			remoteAddr, err = ctl.RegisterProxy(&retContent.NewProxy)
		}

		if err != nil {
			log.Warn("add proxy failed: %s", err)
			return
		}

		metrics.Server.NewProxy(proxyParams.ProxyName, proxyParams.ProxyType)

		resp := &msg.NewProxyResp{
			ProxyName:  proxyParams.ProxyName,
			RemoteAddr: remoteAddr,
			NewProxy:   &retContent.NewProxy,
		}
		ctl.sendCh <- resp
	}

}

func (svr *Service) ClientList(w http.ResponseWriter, r *http.Request) {
	reflect.ValueOf(svr.ctlManager.ctlsByRunID).MapKeys()
}

func (svr *Service) ProxyByType(w http.ResponseWriter, r *http.Request) {
	res := GeneralResponse{Code: 200}
	params := mux.Vars(r)
	proxyType := params["type"]

	defer func() {
		log.Info("Http response [%s]: code [%d]", r.URL.Path, res.Code)
		w.WriteHeader(res.Code)
		if len(res.Msg) > 0 {
			w.Write([]byte(res.Msg))
		}
	}()
	log.Info("Http request: [%s]", r.URL.Path)

	proxyInfoResp := GetProxyInfoResp{}
	proxyInfoResp.Proxies = svr.getProxyByType(proxyType)

	buf, _ := json.Marshal(&proxyInfoResp)
	res.Msg = string(buf)
}

func (svr *Service) getProxyByType(proxyType string) (proxyInfos []*ProxyStatsInfo) {
	proxyStats := mem.StatsCollector.GetProxiesByType(proxyType)
	proxyInfos = make([]*ProxyStatsInfo, 0, len(proxyStats))
	for _, ps := range proxyStats {
		proxyInfo := &ProxyStatsInfo{}
		if pxy, ok := svr.pxyManager.GetByName(ps.Name); ok {
			content, err := json.Marshal(pxy.GetConf())
			if err != nil {
				log.Warn("marshal proxy [%s] conf info error: %v", ps.Name, err)
				continue
			}
			proxyInfo.Conf = getConfByType(ps.Type)
			if err = json.Unmarshal(content, &proxyInfo.Conf); err != nil {
				log.Warn("unmarshal proxy [%s] conf info error: %v", ps.Name, err)
				continue
			}
			proxyInfo.Status = consts.Online
		} else {
			proxyInfo.Status = consts.Offline
		}
		proxyInfo.Name = ps.Name
		proxyInfo.TodayTrafficIn = ps.TodayTrafficIn
		proxyInfo.TodayTrafficOut = ps.TodayTrafficOut
		proxyInfo.CurConns = ps.CurConns
		proxyInfo.LastStartTime = ps.LastStartTime
		proxyInfo.LastCloseTime = ps.LastCloseTime
		proxyInfos = append(proxyInfos, proxyInfo)
	}
	return
}
