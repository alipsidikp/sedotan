package sedotan

import (
	"bytes"
	"fmt"
	gq "github.com/PuerkitoBio/goquery"
	"github.com/eaciit/cast"
	"github.com/eaciit/toolkit"
	"net/http"
	"net/http/cookiejar"
	"reflect"
	"regexp"
	"strings"
	"time"
)

// type AuthTypeEnum int

// const (
// 	AuthType_Session AuthTypeEnum = iota
// 	AuthType_Cookie
// 	AuthType_Basic
// )

type GrabColumn struct {
	Alias     string
	Selector  string
	ValueType string //-- Text, Attr, InnerHtml, OuterHtml
	AttrName  string
}

type Config struct {
	Data         toolkit.M
	URL          string
	CallType     string
	DataSettings map[string]*DataSetting
	FormValues   toolkit.M

	AuthType     string
	AuthUserId   string
	AuthPassword string
	LoginValues  toolkit.M
	LoginUrl     string
	LogoutUrl    string
}

type DataSetting struct {
	RowSelector    string
	RowDeleteCond  toolkit.M
	RowIncludeCond toolkit.M
	ColumnSettings []*GrabColumn
}

type Grabber struct {
	Config

	LastExecuted time.Time

	bodyByte []byte
	Response *http.Response
}

func NewGrabber(url string, calltype string, config *Config) *Grabber {
	g := new(Grabber)
	if config != nil {
		g.Config = *config
	}

	if url != "" {
		g.URL = url
	}

	if calltype != "" {
		g.CallType = calltype
	}

	return g
}

func (c *Config) SetFormValues(parm toolkit.M) {
	c.FormValues = toolkit.M{}.Set("formvalues", parm)
}

func (g *Grabber) GetConfig() (toolkit.M, error) {
	retValue := toolkit.M{}
	parm, found := g.Config.FormValues["formvalues"].(toolkit.M)
	if found {
		for key, val := range parm {
			switch {
			case strings.Contains(val.(string), "time.Now()"):
				// time.Now(), Date2String(YYYYMMDD) = time.Now().Date2String(YYYYMMDD)
				format := ""
				if strings.Contains(val.(string), "Date2String") {
					format = strings.Replace(strings.Replace(val.(string), "time.Now().Date2String(", "", -1), ")", "", -1)
				}

				parm[key] = cast.Date2String(time.Now(), format)
			}
		}

		retValue.Set("formvalues", parm)
	}

	switch {
	case ((g.AuthType == "session" || g.AuthType == "cookie") && g.LoginValues != nil):
		tConfig := toolkit.M{}
		tConfig.Set("loginvalues", g.LoginValues)
		jar, e := toolkit.HttpGetCookieJar(g.LoginUrl, g.CallType, tConfig)
		if e != nil {
			return nil, e
		}

		retValue.Set("cookie", jar)
	}

	return retValue, nil
}

func (ds *DataSetting) Column(i int, column *GrabColumn) *GrabColumn {
	if i == 0 {
		ds.ColumnSettings = append(ds.ColumnSettings, column)
	} else if i <= len(ds.ColumnSettings) {
		ds.ColumnSettings[i-1] = column
	} else {
		return nil
	}
	return column
}

func (g *Grabber) Data() interface{} {
	return nil
}

func (g *Grabber) DataByte() []byte {
	d := g.Data()
	if toolkit.IsValid(d) {
		return toolkit.Jsonify(d)
	}
	return []byte{}
}

func (g *Grabber) Grab(parm toolkit.M) error {
	errorTxt := ""

	sendConf, e := g.GetConfig()
	if e != nil {
		return fmt.Errorf("Unable to grab %s, GetConfig Error found %s", g.URL, e.Error())
	}

	r, e := toolkit.HttpCall(g.URL, g.CallType, g.DataByte(), sendConf)
	if e != nil {
		errorTxt = e.Error()
	} else if r.StatusCode != 200 {
		errorTxt = r.Status
	}

	if errorTxt != "" {
		return fmt.Errorf("Unable to grab %s. %s", g.URL, errorTxt)
	}

	g.Response = r
	g.bodyByte = toolkit.HttpContent(r)

	//Logout ====
	if sendConf.Has("cookie") {
		tjar := sendConf.Get("cookie", nil).(*cookiejar.Jar)
		if tjar != nil && g.LogoutUrl != "" {
			_, e := toolkit.HttpCall(g.LogoutUrl, g.CallType, g.DataByte(), toolkit.M{}.Set("cookie", tjar))
			if e != nil {
				return fmt.Errorf("Unable to logout %s, grab logout Error found %s", g.LogoutUrl, e.Error())
			}
		}
	}

	return nil
}

func (g *Grabber) ResultString() string {
	if g.Response == nil {
		return ""
	}
	return string(g.bodyByte)
}

func (g *Grabber) ResultFromHtml(dataSettingId string, out interface{}) error {

	reader := bytes.NewReader(g.bodyByte)
	doc, e := gq.NewDocumentFromReader(reader)
	if e != nil {
		return e
	}

	ms := []toolkit.M{}
	records := doc.Find(g.Config.DataSettings[dataSettingId].RowSelector)
	recordCount := records.Length()

	for i := 0; i < recordCount; i++ {
		record := records.Eq(i)
		m := toolkit.M{}
		for cindex, c := range g.Config.DataSettings[dataSettingId].ColumnSettings {
			columnId := fmt.Sprintf("%s", cindex)
			if c.Alias != "" {
				columnId = c.Alias
			}
			sel := record.Find(c.Selector)
			var value interface{}
			valuetype := strings.ToLower(c.ValueType)
			if valuetype == "attr" {
				value, _ = sel.Attr(c.AttrName)
			} else if valuetype == "html" {
				value, _ = sel.Html()
			} else {
				value = sel.Text()
			}
			value = strings.TrimSpace(fmt.Sprintf("%s", value))
			m.Set(columnId, value)
		}

		if !(g.Config.DataSettings[dataSettingId].getDeleteCondition(m)) {
			ms = append(ms, m)
		}
	}
	if edecode := toolkit.Unjson(toolkit.Jsonify(ms), out); edecode != nil {
		return edecode
	}
	return nil
}

func (ds *DataSetting) getDeleteCondition(dataCheck toolkit.M) bool {
	resBool := false

	if len(ds.RowDeleteCond) > 0 {
		resBool = foundCondition(dataCheck, ds.RowDeleteCond)
	}

	if !resBool && len(ds.RowIncludeCond) > 0 {
		resBool = true
		iResBool := true
		iResBool = foundCondition(dataCheck, ds.RowIncludeCond)
		if iResBool {
			resBool = false
		}
	}

	return resBool
}

func foundCondition(dataCheck toolkit.M, cond toolkit.M) bool {
	resBool := true

	for key, val := range cond {
		if key == "$and" || key == "$or" {
			for i, sVal := range val.([]interface{}) {
				rVal := sVal.(map[string]interface{})
				mVal := toolkit.M{}
				for rKey, mapVal := range rVal {
					mVal.Set(rKey, mapVal)
				}

				xResBool := foundCondition(dataCheck, mVal)
				if key == "$and" {
					resBool = resBool && xResBool
				} else {
					if i == 0 {
						resBool = xResBool
					} else {
						resBool = resBool || xResBool
					}
				}
			}
		} else {
			if reflect.ValueOf(val).Kind() == reflect.Map {
				mVal := val.(map[string]interface{})
				tomVal, _ := toolkit.ToM(mVal)
				switch {
				case tomVal.Has("$ne"):
					if tomVal["$ne"].(string) == dataCheck.Get(key, "").(string) {
						resBool = false
					}
				case tomVal.Has("$regex"):
					resBool, _ = regexp.MatchString(tomVal["$regex"].(string), dataCheck.Get(key, "").(string))
				case tomVal.Has("$gt"):
					if tomVal["$gt"].(string) >= dataCheck.Get(key, "").(string) {
						resBool = false
					}
				case tomVal.Has("$gte"):
					if tomVal["$gte"].(string) > dataCheck.Get(key, "").(string) {
						resBool = false
					}
				case tomVal.Has("$lt"):
					if tomVal["$lt"].(string) <= dataCheck.Get(key, "").(string) {
						resBool = false
					}
				case tomVal.Has("$lte"):
					if tomVal["$lte"].(string) < dataCheck.Get(key, "").(string) {
						resBool = false
					}
				}
			} else if reflect.ValueOf(val).Kind() == reflect.String && val != dataCheck.Get(key, "").(string) {
				resBool = false
			}
		}
	}

	return resBool
}
