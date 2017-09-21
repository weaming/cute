package main

import (
	"flag"
	"fmt"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/garyburd/redigo/redis"
	"github.com/mssola/user_agent"
	"github.com/weaming/cute/qqwry"
	"gopkg.in/redsync.v1"
	"log"
	"net"
	"net/http"
	"strings"
)

var (
	listen      = flag.String("listen", ":8080", "listen [host]:port")
	redisServer = flag.String("redis", "127.0.0.1:6379", "redis host:port")
	xRealIP     = flag.String("header", "X-Real-IP", "key of IP in proxy HTTP header")
	redisDB     = flag.Int("db", 0, "redis database")
	datFile     = flag.String("qqwry", "./qqwry.dat", "纯真 IP 库的数据文件路径")
	Pool        = NewRedisPool(*redisServer, "", *redisDB, 20)
)

type Click struct {
	Host       string            `json:"host"`
	URI        string            `json:"uri"`
	IP         string            `json:"ip"`
	IPLocation qqwry.ResultQQwry `json:"location"`

	uaRaw string
	ua    *user_agent.UserAgent
	Click *Stat `json:"click"`

	BrowserFamily string `json:"browser_family"`
	Browser       string `json:"browser"`
	Platform      string `json:"platform"`
	OS            string `json:"os"`

	Bot    bool `json:"is_bot"`
	Mobile bool `json:"is_mobile"`
}

type Stat struct {
	Total         int64 `json:"total"`
	URI           int64 `json:"uri"`
	BrowserFamily int64 `json:"browser_family"`
	Browser       int64 `json:"browser"`
	OS            int64 `json:"os"`
	Platform      int64 `json:"platform"`
	IP            int64 `json:"ip"`
}

func (p *Click) save(pool *redis.Pool) int64 {
	if len(p.uaRaw) > 0 {
		p.ua = user_agent.New(p.uaRaw)
		br, version := p.ua.Browser()
		p.BrowserFamily = br
		p.Browser = br + " " + version

		p.Platform = p.ua.Platform()
		p.OS = p.ua.OS()

		p.Bot = p.ua.Bot()
		p.Mobile = p.ua.Mobile()
	} else {
		p.Browser = "unknown"
	}

	// save
	dis := redsync.New([]redsync.Pool{Pool})
	mutex := dis.NewMutex(p.Host + p.URI)
	mutex.Lock()
	defer mutex.Unlock()

	c := Pool.Get()
	// bool count
	if p.Bot {
		c.Do("HINCRBY", p.Host, "bot", 1)
		c.Do("HINCRBY", "global", "bot", 1)
	}
	if p.Mobile {
		c.Do("HINCRBY", p.Host, "mobile", 1)
		c.Do("HINCRBY", "global", "mobile", 1)
	}

	// transaction
	c.Send("MULTI")
	// origin design target of this project
	c.Send("HINCRBY", p.Host, "uri:"+p.URI, 1)
	// additional metrics of site
	c.Send("HINCRBY", p.Host, "total", 1) // 1
	c.Send("HINCRBY", p.Host, "family:"+p.BrowserFamily, 1)
	c.Send("HINCRBY", p.Host, "browser:"+p.Browser, 1)
	c.Send("HINCRBY", p.Host, "os:"+p.OS, 1)
	c.Send("HINCRBY", p.Host, "platform:"+p.Platform, 1)
	c.Send("HINCRBY", p.Host, "ip:"+p.IP, 1)
	// global
	c.Send("HINCRBY", "global", "total", 1) // 6
	c.Send("HINCRBY", "global", "family:"+p.BrowserFamily, 1)
	c.Send("HINCRBY", "global", "browser:"+p.Browser, 1)
	c.Send("HINCRBY", "global", "os:"+p.OS, 1)
	c.Send("HINCRBY", "global", "platform:"+p.Platform, 1)
	c.Send("HINCRBY", "global", "ip:"+p.IP, 1)

	r, err := c.Do("EXEC")
	if err == nil {
		//fmt.Printf("%v %T\n", r, r)
		if r, ok := r.([]interface{}); ok {
			// init
			p.Click = &Stat{}

			p.Click.URI = r[0].(int64)

			p.Click.Total = r[1].(int64)
			p.Click.BrowserFamily = r[2].(int64)
			p.Click.Browser = r[3].(int64)
			p.Click.OS = r[4].(int64)
			p.Click.Platform = r[5].(int64)
			p.Click.IP = r[6].(int64)

			return p.Click.URI
		}
		return -500
	} else {
		logE(err)
		return -400
	}
}

func getConnectIP(r *rest.Request) (ip string) {
	if strings.Contains(r.RemoteAddr, "[") {
		// IPv6
		ip = "ipv6"
	} else {
		// IPv4
		ip = strings.SplitN(r.RemoteAddr, ":", 2)[0]
		return ip
	}
	return ""
}

func OnClick(w rest.ResponseWriter, r *rest.Request) {
	// site
	q := r.URL.Query()
	var host, uri string
	if hosts, ok := q["host"]; ok {
		host = hosts[0]
	}
	if uris, ok := q["uri"]; ok {
		uri = uris[0]
	}
	if host == "" || uri == "" {
		w.WriteHeader(400)
		w.WriteJson(map[string]string{"error": "missing query parameter host or uri"})
		return
	}

	// client
	ip := r.Header.Get(*xRealIP)
	if ip == "" {
		// get from connect
		ip = getConnectIP(r)
	} else {
		// get from http header
		if x := net.ParseIP(ip); x != nil {
			ip = x.String()
		} else {
			log.Fatal("proxy ip format invalid: " + ip)
		}
	}

	qq := qqwry.NewQQwry()
	location := qq.Find(ip)

	c := Click{
		Host:       host,
		URI:        uri,
		IP:         ip,
		IPLocation: location,
	}
	c.uaRaw = r.UserAgent()

	c.save(Pool)
	w.WriteJson(c)
}

func IPQuery(w rest.ResponseWriter, r *rest.Request) {
	ip := r.PathParam("ip")

	// get IP from tcp or http headers
	if ip == "" {
		// get from connect
		ip = getConnectIP(r)
	}
	if ip == "" || ip == "ipv6" {
		// get from http header
		ip := r.Header.Get(*xRealIP)

		if x := net.ParseIP(ip); x != nil {
			ip = x.String()
		} else {
			log.Fatal("proxy ip format invalid: " + ip)
		}
	}

	qq := qqwry.NewQQwry()
	location := qq.Find(ip)
	w.WriteJson(location)
}

func GetIP(w rest.ResponseWriter, r *rest.Request) {
	// get from connect
	ip := getConnectIP(r)
	if ip == "" || ip == "ipv6" {
		// get from http header
		ip := r.Header.Get(*xRealIP)

		if x := net.ParseIP(ip); x != nil {
			ip = x.String()
		} else {
			w.WriteJson(map[string]string{"error": "proxy ip format invalid, maybe blank or ipv6"})
			return
		}
	}

	qq := qqwry.NewQQwry()
	location := qq.Find(ip)
	w.WriteJson(location)
}

func Index(w rest.ResponseWriter, r *rest.Request) {
	host := r.PathParam("host")
	c := Pool.Get()
	if len(host) > 0 {
		if dict, err := redis.StringMap(c.Do("HGETALL", host)); err == nil {
			if len(dict) == 0 {
				w.WriteHeader(404)
				w.WriteJson(map[string]string{"error": fmt.Sprintf("key %v not exits\n", host)})
				return
			}
			w.WriteJson(dict)
		} else {
			logE(err)
			w.WriteJson(map[string]string{"error": err.Error(), "host": host})
		}
	} else {
		if global, err := redis.StringMap(c.Do("HGETALL", "global")); err == nil {
			w.WriteJson(global)
		} else {
			logE(err)
			w.WriteJson(map[string]string{"error": err.Error(), "host": host})
		}
	}
}

func init() {
	flag.Parse()
	// load qqwry data to memory
	qqwry.IPData.InitIPData(*datFile)
}

func main() {
	api := rest.NewApi()
	api.Use(rest.DefaultDevStack...)
	api.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT"},
		AllowedHeaders: []string{
			"Accept", "Content-Type", "Origin"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})
	if router, err := rest.MakeRouter(
		rest.Get("/click", OnClick),
		rest.Get("/ip", GetIP),
		rest.Get("/ip/#ip", IPQuery),
		rest.Get("/host/#host", Index),
		rest.Get("/", Index),
	); err != nil {
		log.Fatal(err)
	} else {
		api.SetApp(router)
	}
	if (*listen)[0] == ':' {
		log.Printf("listen on http://127.0.0.1%v, and public ip too!\n", *listen)
	} else {
		log.Printf("listen on http://%v\n", *listen)
	}
	log.Fatal(http.ListenAndServe(*listen, api.MakeHandler()))
}
