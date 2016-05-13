package models

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/deepzz0/go-common/log"
	db "github.com/deepzz0/go-common/mongo"
	tm "github.com/deepzz0/go-common/time"
	"github.com/deepzz0/go-common/useragent"
	"github.com/wangtuanjie/ip17mon"
	"gopkg.in/mgo.v2/bson"
)

///////////////////////////////////////////////////////////////////////////
type Leftbar struct {
	ID    string // 内部ID
	Title string // 说明
	Extra string // 链接
	Text  string // 显示名称
}

///////////////////////////////////////////////////////////////////////////
type Request struct {
	Referer    string               // 请求来源
	URL        string               // 访问页面
	Major      int                  // 主版本
	RemoteAddr string               // 请求IP
	SessionID  string               // 请求session
	UserAgent  *useragent.UserAgent //
	Time       time.Time            // 请求时间
}

func NewRequest(r *http.Request) *Request {
	request := &Request{Time: time.Now()}
	request.Referer = r.Referer()
	request.URL = r.URL.String()
	request.Major = r.ProtoMajor
	request.RemoteAddr = r.RemoteAddr[:strings.LastIndex(r.RemoteAddr, ":")]
	request.UserAgent = useragent.ParseByRequest(r)
	sessionid, err := r.Cookie("SESSIONID")
	if err != nil {
		log.Error(err)
	}
	request.SessionID = sessionid.Value
	return request
}

type RequestManage struct {
	// lock sync.RWMutex
	// SSidToTime map[string]time.Time
	Ch chan *Request
}

var RequestM = NewRequestM()

func NewRequestM() *RequestManage {
	return &RequestManage{Ch: make(chan *Request, 20)}
}

func (m *RequestManage) Saver() {
	t := time.NewTicker(time.Minute * 10)
	for {
		select {
		case request := <-m.Ch:
			err := db.Insert(DB, C_REQUEST, request)
			if err != nil {
				log.Error(err)
			}

		case <-t.C:
			ManageData.loadData(TODAY)
		}
	}
}

///////////////////////////////////////////////////////////////////////////
const (
	YESTERDAY = "yesterday"
	TODAY     = "today"
)

var ManageData = NewBaseData()

type BaseData struct {
	lock      sync.RWMutex
	PV        map[string]int
	UV        map[string]int
	IP        map[string]int
	TimePV    map[string][]int
	EngineTop map[string]int
	PageTop   map[string]int
	China     map[string]*Area
	World     map[string]*Area
	Latest    []*Request
}

type Area struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func NewBaseData() *BaseData {
	bd := &BaseData{PV: make(map[string]int), UV: make(map[string]int), IP: make(map[string]int), TimePV: make(map[string][]int)}
	return bd
}

const (
	pageCount = 30
)

func (b *BaseData) loadData(typ string) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	ms, c := db.Connect(DB, C_REQUEST)
	c.EnsureIndexKey("time")
	defer ms.Close()

	now := tm.New(time.Now())
	var Begin, End time.Time
	if typ == TODAY {
		Begin = now.BeginningOfDay()
		End = now.EndOfDay()
	} else if typ == YESTERDAY {
		Begin = now.BeginningOfDay().Add(-24 * time.Hour)
		End = now.EndOfDay().Add(-24 * time.Hour)
	}
	if typ == TODAY {
		err := c.Find(nil).Sort("-time").Skip(0).Limit(pageCount).All(&b.Latest)
		if err != nil {
			log.Error(err)
		}
	}
	count, err := c.Find(bson.M{"time": bson.M{"$gte": Begin, "$lt": End}}).Count()
	if err != nil {
		log.Error(err)
	}
	b.PV[typ] = count
	var sessions []string
	err = c.Find(bson.M{"time": bson.M{"$gte": Begin, "$lt": End}}).Distinct("sessionid", &sessions)
	if err != nil {
		log.Error(err)
	}
	b.UV[typ] = len(sessions)
	var ips []string
	err = c.Find(bson.M{"time": bson.M{"$gte": Begin, "$lt": End}}).Distinct("remoteaddr", &ips)
	if err != nil {
		log.Error(err)
	}
	b.China = make(map[string]*Area)
	b.World = make(map[string]*Area)
	for _, v := range ips {
		info, err := ip17mon.Find(v)
		if err != nil {
			log.Error(err)
			continue
		}
		if info.Country == "中国" {
			if city := b.China[info.City]; city == nil {
				b.China[info.City] = &Area{Name: info.Region, Value: 1}
			} else {
				city.Value++
			}
		} else {
			if country := b.World[info.Country]; country == nil {
				b.World[info.Country] = &Area{Name: info.Country, Value: 1}
			} else {
				country.Value++
			}
		}
	}

	b.IP[typ] = len(ips)
	var ts []*Request
	err = c.Find(bson.M{"time": bson.M{"$gte": Begin, "$lt": End}}).Select(bson.M{"time": 1}).All(&ts)
	if err != nil {
		log.Error(err)
	}
	b.TimePV[typ] = make([]int, 145)
	for _, v := range ts {
		b.TimePV[typ][ParseTime(v.Time)]++
	}
}

func (b *BaseData) LoadData() {
	b.loadData(TODAY)
	b.loadData(YESTERDAY)
}

func ParseTime(t time.Time) int { // 第几个十分钟
	return (t.Hour()*60+t.Minute())/10 + 1
}

///////////////////////////////////////////////////////////////////////////
type Verification struct {
	Name       string // pk
	Content    string
	CreateTime time.Time
}

func NewVerify() *Verification {
	return &Verification{CreateTime: time.Now()}
}

var ManageConf = LoadConf()

type Config struct {
	Name       string
	SiteVerify map[string]*Verification
}

func LoadConf() *Config {
	conf := &Config{Name: "config", SiteVerify: make(map[string]*Verification)}
	err := db.FindOne(DB, C_CONFIG, bson.M{"name": "config"}, conf)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		log.Error(err)
	}
	return conf
}

func (conf *Config) GetVerification(name string) *Verification {
	return conf.SiteVerify[name]
}

func (conf *Config) AddVerification(verify *Verification) {
	conf.SiteVerify[verify.Name] = verify
}

func (conf *Config) DelVerification(name string) {
	conf.SiteVerify[name] = nil
	delete(conf.SiteVerify, name)
}

func (conf *Config) UpdateConf() {
	err := db.Update(DB, C_CONFIG, bson.M{"name": "config"}, bson.M{"$set": bson.M{"siteverify": conf.SiteVerify}})
	if err != nil {
		log.Error(err)
	}
}
