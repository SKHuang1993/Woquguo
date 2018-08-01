package fare

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errorlog"
	"errors"
	"fmt"
	"io/ioutil"
	"mysqlop"
	"net/http"
	"outsideapi"
	"sort"
	"strconv"
	"strings"
	"sync"
	"webapi"
	"cachestation"
)

var stopAccept bool //被Daemon(),WAPI_AcceptGDSFare()使用
/***************多服务端并行服务接口*************/
//Fare==MainBill
func ReloadB2Fare(w http.ResponseWriter, r *http.Request) {

	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var err error

	defer func() {
		if err != nil {
			fmt.Fprint(w, errorlog.Result(false))
		} else {
			fmt.Fprint(w, errorlog.Result(true))
		}
	}()

	var ts mysqlop.TotleService
	if err = json.Unmarshal(result, &ts); err != nil {
		return
	}

	if ts.Deal == "Reload" {

		errorlog.WriteErrorLog("{{Syscall}} ReloadB2Fare (1): " + ts.Code)

		//类似于折扣代码那块。先删除，再刷新加载最新
		//删除某一条票单
		MainBillDelete(ts.Code)
		MainBillLoad(ts.Code)


	} else {
		err = errors.New("Err Param")
	}
}


func DeleteB2Fare(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()

	result, _ := ioutil.ReadAll(r.Body)

	var err error

	defer func() {
		if err != nil {
			fmt.Fprint(w, errorlog.Result(false))
		} else {
			fmt.Fprint(w, errorlog.Result(true))
		}
	}()

	var ts mysqlop.TotleService
	if err = json.Unmarshal(result, &ts); err != nil {
		return
	}

	if ts.Deal == "Delete" {
		errorlog.WriteErrorLog("{{Syscall}} DeleteB2Fare (1): " + ts.Code)
		//和上面ReloadB2Fare有点类似。这里仅仅是删除MainBill;而上面是需要先删除再刷新
		MainBillDelete(ts.Code)

	} else {
		err = errors.New("Err Param")
	}
}

func DeleteAllB2Fare(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var err error

	defer func() {
		if err != nil {
			fmt.Fprint(w, errorlog.Result(false))
		} else {
			fmt.Fprint(w, errorlog.Result(true))
		}
	}()

	var ts mysqlop.TotleService
	if err = json.Unmarshal(result, &ts); err != nil {
		return
	}


	if ts.Deal == "Delete" {
		errorlog.WriteErrorLog("{{Syscall}} DeleteAllB2Fare (1): " + ts.Code)
		var wg sync.WaitGroup
		wg.Add(2)

		deleteQS := func(this *QueryStation) {
			defer wg.Done()
			this.mutex.RLock()
			for _, departOther := range QueryDepart.DepartStation {
				departOther.mutex.RLock()
				for _, datesfares := range departOther.ArriveStation {
					datesfares.Delete(4, "")
				}
				departOther.mutex.RUnlock()
			}
			this.mutex.RUnlock()
		}

		//删除QS
		go deleteQS(&QueryDepart)
		go deleteQS(&QueryArrive)
		wg.Wait()

	} else {
		err = errors.New("Err Param")
	}
}

/************fare查询应用接口*******************/
//Point2Point查询(同时用于内存检测查询)
func WAPI_QueryFare(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()

	result, _ := ioutil.ReadAll(r.Body)

	var ListFare mysqlop.MutilDaysMainBill

	defer func() {
		fmt.Fprint(w, errorlog.Make_JSON_GZip_Reader(ListFare))
	}()

	var p2p_in webapi.Point2Point_In

	if err := json.Unmarshal(result, &p2p_in); err != nil {
		errorlog.WriteErrorLog("WAPI_QueryFare (1): " + string(result))
		return
	}

	if p2p_in.Days < 0 || p2p_in.Days > 60 {
		return
	}

	if p2p_in.Days == 0 {
		p2p_in.Days = 1
	}

	if len(p2p_in.Rout) == 0 {
		p2p_in.Rout = webapi.FlightLegs2Routine(p2p_in.Flight)
	}

	if len(p2p_in.Rout) == 0 || len(p2p_in.Rout) > 2 {
		return
	}

	//这个cListFare 其实就是用来接收数据的
	cListFare := make(chan mysqlop.MutilDaysMainBill, 1)


	//如果Rout大于1，多个行程。而且Days也大于1。。则进行多天查询
	if len(p2p_in.Rout) > 1 && p2p_in.Days > 1 {
		MutilQueryFare(&p2p_in, cListFare)
	} else {
		QueryFare(&p2p_in, "", cListFare)
	}

	ListFare = <-cListFare
}

//航班查询*路线检测
func WAPI_RoutineCheckSelect(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	//var err error
	var qcs webapi.QCS

	defer func() {
		b, _ := json.Marshal(qcs)
		fmt.Fprint(w, bytes.NewBuffer(b))
		//fmt.Fprint(w, errorlog.Make_JSON_GZip_Reader(qcs))
	}()

	var p2p_in webapi.Point2Point_In

	if err := json.Unmarshal(result, &p2p_in); err != nil {
		errorlog.WriteErrorLog("WAPI_RoutineCheckSelect (1): " + string(result))
		return
	}

	if len(p2p_in.Rout) == 0 {
		p2p_in.Rout = webapi.FlightLegs2Routine(p2p_in.Flight)
	}
	//rout := webapi.FlightLegs2Routine(p2p_in.Flight)
	if len(p2p_in.Rout) == 0 {
		return
	}

	ConnStations := make([]*webapi.Conns, 0, 2)

	leg := p2p_in.Rout[0] //来回程其实一样的,另外快速shopping直接要使用这个结果.
	//for _, leg := range rout {
	for _, depart := range leg.DepartCounty {
		for _, arrive := range leg.ArriveCounty {
			if connStation, havefare, err := RoutineCheckSelect(depart, arrive, leg.TravelDate); err == nil {
				if havefare == true {
					qcs.Havefare = true
					return
				} else if len(connStation) > 0 {
					ConnStations = append(ConnStations, &webapi.Conns{
						DepartStation: depart,
						ArriveStation: arrive,
						ConnStation:   connStation})
				}
			}
		}
	}

	qcs.ConnStations = ConnStations
}

/******************翻译规则********************/
//新增翻译条款
func WAPI_AddRule(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var err error
	var ID int
	var RIT struct {
		Item     string `json:"Item"`  //条款（28项）传入其中1项
		Airline  string `json:"Airline"`  //航司
		EnColumn string `json:"EnColumn"` //中文字段
		CnColumn string `json:"CnColumn"` //英文字段
	}

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}
		body, _ := json.Marshal(struct {
			ID       int    `json:"ID"`   //ID 这条数据在数据库里面的一个ID
			ErrorStr string `json:"ErrorStr"`  //如果为空，代表正确。有内容，代表错误
		}{
			ID:       ID,
			ErrorStr: errstr})

		fmt.Fprint(w, string(body))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	if err = json.Unmarshal(result, &RIT); err != nil {
		return
	}

	if len(RIT.EnColumn) > 0 && RIT.EnColumn[len(RIT.EnColumn)-1] == '.' {
		RIT.EnColumn = RIT.EnColumn[:len(RIT.EnColumn)-1]
	}
	if len(RIT.CnColumn) > 0 && RIT.CnColumn[len(RIT.CnColumn)-1] == '.' {
		RIT.CnColumn = RIT.CnColumn[:len(RIT.CnColumn)-1]
	}

	RIT.EnColumn = reducePlace(RIT.EnColumn)
	RIT.CnColumn = reducePlace(RIT.CnColumn)

	ID = getRID()
	if err = RuleTotalParse.AddRule(ID, RIT.Item, RIT.Airline, RIT.EnColumn, RIT.CnColumn); err == nil {
		if err = RuleTranslateInsert(ID, RIT.Item, RIT.Airline, RIT.EnColumn, RIT.CnColumn); err != nil {
			RuleTotalParse.DeleteRule(ID, RIT.Item)
		}
	}
}

func WAPI_DeleteRule(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var err error

	var RIT struct {
		ID   int    `json:"ID"`
		Item string `json:"Item"`
	}

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}
		body, _ := json.Marshal(struct {
			ErrorStr string `json:"ErrorStr"`
		}{ErrorStr: errstr})

		fmt.Fprint(w, string(body))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	if err = json.Unmarshal(result, &RIT); err != nil {
		return
	}

	if err = RuleTranslateDelete(RIT.ID); err == nil {
		RuleTotalParse.DeleteRule(RIT.ID, RIT.Item)
	}
}

func WAPI_QueryRule(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var err error
	var body *bytes.Buffer
	var RIT struct {
		Item    string `json:"Item"`
		Airline string `json:"Airline"`
	}

	defer func() {
		fmt.Fprint(w, body)
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	if err = json.Unmarshal(result, &RIT); err != nil {
		return
	}

	body, err = QueryRule(RIT.Item, RIT.Airline)
}

//单句翻译测试
func WAPI_TestRule(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var CnColumn string
	var err error
	var RIT struct {
		Item    string `json:"Item"` //编号
		Airline string `json:"Airline"` //航司
		Content string `json:"Content"` //中文
	}

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}
		body, _ := json.Marshal(struct {
			CnColumn string `json:"CnColumn"`  //翻译后的中文内容
			ErrorStr string `json:"ErrorStr"` //错误信息
		}{
			CnColumn: CnColumn,
			ErrorStr: errstr})

		fmt.Fprint(w, string(body))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	if err = json.Unmarshal(result, &RIT); err != nil {
		return
	}

	CnColumn, err = RuleTotalParse.SentenceParse(RIT.Content, RIT.Item, RIT.Airline)
}

/******翻译内容(主要用于测试内容的提取和整体翻译)*****/
func WAPI_TestRuleContent(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var BeforeTranslate []string
	var TransContent []string
	var err error

	var RIT struct {
		Item    string `json:"Item"`     //条款
		Airline string `json:"Airline"`  //航司
		Content string `json:"Content"`  //整段内容
	}

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}
		body, _ := json.Marshal(struct {
			BeforeTranslate []string `json:"BeforeTranslate"`  //多少是可被翻译的
			TransContent    []string `json:"TransContent"`  //每一行，相对于上一行的翻译结果
			ErrorStr        string   `json:"ErrorStr"`
		}{
			BeforeTranslate: BeforeTranslate,
			TransContent:    TransContent,
			ErrorStr:        errstr})

		fmt.Fprint(w, string(body))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	if err = json.Unmarshal(result, &RIT); err != nil {
		return
	}

	rule := new(Rule)
	rule.Init(RIT.Content, RIT.Airline)
	BeforeTranslate, TransContent, err = rule.SubItemParse(RIT.Item)
}

/******************命令*************************/
//加入命令,并启动服务...
func WAPI_CommandInsert(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()
	//如果查询正确的话，会返回下面这个东西
	/*
	{"Obj":{"ID":540,"CommandStr":"get -depart -city CAN -arrive -city BJS  -date 2018-10-01 -route RT -airline CX","Status":0,"Operator":"SKHuang","InsertTime":"2018-07-27 12:14:21"},"ErrorStr":""}
	*/

	var (
		err     error
		command string
		obj     interface{}
		o_mq    *MessQueue

		Comm struct {
			CommandStr string `json:"CommandStr"`  //命令
			Operator   string `json:"Operator"`   //谁操作
		}
	)

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}
		body, _ := json.Marshal(struct {
			Obj      interface{} `json:"Obj"`   //其实是一个ID，就是插入成功后返回的ID
			ErrorStr string      `json:"ErrorStr"`
		}{
			Obj:      obj,
			ErrorStr: errstr})

		fmt.Fprint(w, string(body))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	if err = json.Unmarshal(result, &Comm); err != nil {
		return
	}


	//进行检查我输入的命令
	if command, err = CommandCheck(Comm.CommandStr); err == nil {
		mq := &MessQueue{
			Command:    Comm.CommandStr,
			Status:     0,
			Operator:   Comm.Operator,
			InsertTime: errorlog.DateTime()}

		switch command {
		//查询
		case "select": //直接操作
			o_id, _ := strconv.Atoi(strings.Split(mq.Command, " ")[2])
			if o_mq, err = CommandSuper(o_id); err == nil {
				if _, departStations, arriveStations, _, _, _, _, _, err := CommandParse(o_mq.Command, true); err == nil {

					//#TODO 这个接口没人使用(7.27升哥新修改)
					obj = GdsFareQuery(	o_id/*o_mq.ID*/, departStations, arriveStations)

					mq.Status = 3
				} else {
					mq.Status = 2
				}
			}

			//删除
		case "delete": //直接操作
			mq.ID = getCID()
			o_id, _ := strconv.Atoi(strings.Split(mq.Command, " ")[2])
			if o_mq, err = CommandSuper(o_id); err == nil {
				if _, departStations, arriveStations, _, _, _, _, _, err := CommandParse(o_mq.Command, true); err == nil {
					GdsFareDelete(o_mq.ID, departStations, arriveStations)
					mq.Status = 3
				} else {
					mq.Status = 2
				}

				obj = mq
				//保存进数据库
				if err = CommandInsert(mq.ID, mq.Command, mq.Status, mq.Operator, mq.InsertTime); err == nil {
					//缓存所有命令
					CommandCache.mutex.Lock()
					CommandCache.Command[mq.ID] = mq
					CommandCache.mutex.Unlock()
				}
			}


		case "get", "update": //进入命令队列
			mq.ID = getCID()
			obj = mq

			if err = commMission.Insert(mq); err == nil {
				//保存进数据库
				err = CommandInsert(mq.ID, mq.Command, mq.Status, mq.Operator, mq.InsertTime)

				//缓存所有命令
				CommandCache.mutex.Lock()
				CommandCache.Command[mq.ID] = mq
				CommandCache.mutex.Unlock()

				if len(commMission.exec) < MaxCommandDeamon {
					commMission.Exec() //启动一个处理线程,这里未考虑其他处理线程是否空闲.
				}
			}
		}
	}
}

//删除命令,未完成的非立即执行命令.
func WAPI_CommandDelete(w http.ResponseWriter, r *http.Request) {


	defer errorlog.DealRecoverLog()

	var err error
	var Comm struct {
		ID int `json:"ID"`
	}

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}
		body, _ := json.Marshal(struct {
			ErrorStr string `json:"ErrorStr"`
		}{
			ErrorStr: errstr})

		fmt.Fprint(w, string(body))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	if err = json.Unmarshal(result, &Comm); err != nil {
		return
	}

	mq := &MessQueue{
		ID: Comm.ID}

	if err = commMission.Delete(mq); err == nil {
		err = CommandDelete(mq.ID)
	}
}

//查询接口,返回所有的记录(包含完成的)
func WAPI_CommandSelect(w http.ResponseWriter, r *http.Request) {


	defer errorlog.DealRecoverLog()

	var err error
	obj := make(ListMessQueue, 0, len(CommandCache.Command))

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}

		fmt.Fprint(w, errorlog.Make_JSON_GZip_Reader(struct {
			Obj      interface{} `json:"Obj"`  // 一个列表
			ErrorStr string      `json:"ErrorStr"`
		}{
			Obj:      obj,
			ErrorStr: errstr}))
	}()


	for _, mq := range CommandCache.Command {
		obj = append(obj, mq)
	}

	sort.Sort(obj)
}



//这个是在进行中的命令接口
//查询接口,返回命令队列的记录(未包含完成的)
func WAPI_CommandQuery(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var err error
	obj := make([]*MessQueue, 0, commMission.count+len(doingCommand.queue))

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}

		fmt.Fprint(w, errorlog.Make_JSON_GZip_Reader(struct {
			Obj      interface{} `json:"Obj"`  //一个列表
			ErrorStr string      `json:"ErrorStr"`
		}{
			Obj:      obj,
			ErrorStr: errstr}))
	}()

	for _, mq := range doingCommand.queue {
		obj = append(obj, mq)
	}

	commMission.mutex.RLock()
	for head := commMission.head; head != nil; head = head.next {
		obj = append(obj, head)
	}
	commMission.mutex.RUnlock()
}

/****************条款辅助接口****************/
//这个命令做完之后的票价
//根据Airline,Departure,Arrival,FareBase查询条款内容
func WAPI_CommandStrIndexQuery(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()
	var context []byte
	var err error

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}

		fmt.Fprint(w, errorlog.Make_JSON_GZip_Reader(struct {
			Obj      interface{} `json:"Obj"`  //返回MainBill（一个结构，查）
			ErrorStr string      `json:"ErrorStr"`
		}{
			Obj:      string(context),
			ErrorStr: errstr}))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var CSI struct {
		Airline   string `json:"Airline"`  //航司
		Departure string `json:"Departure"`  //出发
		Arrival   string `json:"Arrival"`//到达
		FareBase  string `json:"FareBase"` //航司条款编号
	}

	if err = json.Unmarshal(result, &CSI); err != nil {
		return
	}


	//通过文件名，在本地查找对应的文件
	filename := "/home/wds/KSFare/" + CSI.Departure + CSI.Arrival + CSI.Airline + strings.Replace(CSI.FareBase, "/", "_", -1) + ".sn"

	context, err = ioutil.ReadFile(filename)
}

//返回之后把远程查了之后的原始文件同时返回.
//比WAPI_CommandStrIndexQuery返回更多的内容
func WAPI_CommandStrIndexQuery_V2(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var context []byte
	var err error

	RetObj := &struct {
		XSFSN string `json:"XSFSN"` //票价
		XSFSL string `json:"XSFSL"` //航线
		XSFXS string `json:"XSFXS"` //有效舱位
		*RuleRecord  //条款内容
	}{}

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}

		fmt.Fprint(w, errorlog.Make_JSON_GZip_Reader(struct {
			Obj      interface{} `json:"Obj"`
			ErrorStr string      `json:"ErrorStr"`
		}{
			Obj:      RetObj,
			ErrorStr: errstr}))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var CSI struct {
		Airline   string `json:"Airline"`
		Departure string `json:"Departure"`
		Arrival   string `json:"Arrival"`
		FareBase  string `json:"FareBase"`
	}

	if err = json.Unmarshal(result, &CSI); err != nil {
		return
	}

	filename := "/home/wds/KSFare/" + CSI.Departure + CSI.Arrival + CSI.Airline + strings.Replace(CSI.FareBase, "/", "_", -1)
	if context, err = ioutil.ReadFile(filename + ".sn"); err == nil {
		RetObj.XSFSN = string(context)
	}

	if context, err = ioutil.ReadFile(filename + ".sl"); err == nil {
		RetObj.XSFSL = string(context)
	}

	if context, err = ioutil.ReadFile(filename + ".xs"); err == nil {
		RetObj.XSFXS = string(context)
	}

	RetObj.RuleRecord, err = RuleConditionLoadLocalOne(CSI.Departure + CSI.Arrival + CSI.Airline + strings.Replace(CSI.FareBase, "/", "_", -1))
}

//查询某一命令的条款列表,这个命令会去拿票单的一些条款
func WAPI_ConditionIndexList(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	errorlog.WriteErrorLog("WAPI_ConditionIndexList (1): ")

	var (
		err        error
		CommandStr string
		Index      int
		Airline    string
		Departure  string
		Arrival    string
		FareBase   string
	)

	type Obj struct {
		CommandStr string `json:"CommandStr"`
		Index      int    `json:"Index"`
		Airline    string `json:"Airline"`
		Departure  string `json:"Departure"`
		Arrival    string `json:"Arrival"`
		FareBase   string `json:"FareBase"`
	}
	var obj []*Obj

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}

		fmt.Fprint(w, errorlog.Make_JSON_GZip_Reader(struct {
			Obj      interface{} `json:"Obj"`
			ErrorStr string      `json:"ErrorStr"`
		}{
			Obj:      obj,
			ErrorStr: errstr}))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var CID struct {
		CommandID int `json:"CommandID"` //id
	}

	if err = json.Unmarshal(result, &CID); err != nil || CID.CommandID == 0 {
		return
	}

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return
	}
	defer conn.Close()

	sqlSelect := "Select CommandStr,IndexID,Airline,Departure,Arrival,FareBase From GDSFare Where CommandID=?"
	row, b := mysqlop.MyQuery(conn, sqlSelect, CID.CommandID)
	if !b {
		err = errors.New("Query Error")
		return
	}
	defer row.Close()

	for row.Next() {
		if err = row.Scan(&CommandStr, &Index, &Airline, &Departure, &Arrival, &FareBase); err == nil {
			obj = append(obj, &Obj{CommandStr, Index, Airline, Departure, Arrival, FareBase})
		}
	}
}





//GDSFare发布接口(Status from 4 to 0)
func WAPI_PublishGDSFare(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	errorlog.WriteErrorLog("WAPI_PublishGDSFare (1): ")

	var err error
	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}
		body, _ := json.Marshal(struct {
			ErrorStr string `json:"ErrorStr"`
		}{
			ErrorStr: errstr})

		fmt.Fprint(w, string(body))
	}()


	//其实就是QueryParam 这个字段 里面有Param 这个参数。这个参数里面存的都是数组
	var QueryParam struct {
		Param []struct {
			Departure string `json:"Departure"`
			Arrival   string `json:"Arrival"`
			Airline   string `json:"Airline"`
			FareBase  string `json:"FareBase"`
		} `json:"Param"`
	}

	map_routine := make(map[string]struct{}, 200)

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	if err = json.Unmarshal(result, &QueryParam); err != nil {
		return
	}

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return
	}
	defer conn.Close()

	sqlSelect := `Select CommandID,FareType,Departure,Arrival,Airline,TripType,Currency,CommandStr,
		IndexID,TravelFirstDate,TravelLastDate,BookingClass,Cabin,FareBase,WeekLimit,MinStay,MaxStay,ApplyAir,NotFitApplyAir,
		AdultPrice,ChildPrice,GDS,Agency,PriceOrder,ApplyRoutine,TravelDate,BackDate,NotTravel,
		AdvpTicketing,AdvpResepvations,RemarkText,IsRt,FirstSalesDate,LastSalesDate,NumberOfPeople,Status
		From GDSFare Where Departure=? And Arrival=? And Airline=? And FareBase=? And Status=4`

	sqlUpdate := `Update GDSFare Set Status=0 Where Departure=? And Arrival=? And Airline=? And FareBase=? And Status=4`

	var gdsfare *outsideapi.GDSFare
	for _, p := range QueryParam.Param {
		if _, ok := map_routine[p.Departure+p.Arrival+p.Airline+p.FareBase]; ok {
			continue
		}
		map_routine[p.Departure+p.Arrival+p.Airline+p.FareBase] = struct{}{}

		row, b := mysqlop.MyQuery(conn, sqlSelect, p.Departure, p.Arrival, p.Airline, p.FareBase)
		if !b {
			err = errors.New("Query Error")
			return
		}

		for row.Next() {
			gdsfare = new(outsideapi.GDSFare)
			gdsfare.FareCmdInfo = new(outsideapi.FareCmdInfo)
			//这里会返回多条记录,因为舱位代码可能不同
			err = row.Scan(&gdsfare.CommandID, &gdsfare.FareType, &gdsfare.Departure, &gdsfare.Arrival, &gdsfare.Airline, &gdsfare.TripType, &gdsfare.Currency, &gdsfare.CommandStr,
				&gdsfare.Index, &gdsfare.TravelFirstDate, &gdsfare.TravelLastDate, &gdsfare.BookingClass, &gdsfare.Cabin, &gdsfare.FareBase, &gdsfare.WeekLimit, &gdsfare.MinStay, &gdsfare.MaxStay, &gdsfare.ApplyAir, &gdsfare.NotFitApplyAir,
				&gdsfare.AdultPrice, &gdsfare.ChildPrice, &gdsfare.GDS, &gdsfare.Agency, &gdsfare.PriceOrder, &gdsfare.ApplyRoutine, &gdsfare.TravelDate, &gdsfare.BackDate, &gdsfare.NotTravel,
				&gdsfare.AdvpTicketing, &gdsfare.AdvpResepvations, &gdsfare.Remark, &gdsfare.IsRt, &gdsfare.FirstSalesDate, &gdsfare.LastSalesDate, &gdsfare.NumberOfPeople, &gdsfare.Status)

			if err == nil {
				gdsfare.Status = 0
				//Cache Fare
				if gdsfare.TravelLastDate >= errorlog.Today() { //TravelLastDate的格式是这样吗?
					for _, bill := range Copy(gdsfare) {
						b2fareCache(&QueryDepart, bill.Springboard, bill.Destination, bill, false)
						b2fareCache(&QueryArrive, bill.Destination, bill.Springboard, bill, false)
					}
				}
			}
		}
		row.Close()
		mysqlop.MyExec(conn, sqlUpdate, p.Departure, p.Arrival, p.Airline, p.FareBase)
	}
}

//接口系统抛过来的数据
//#TODO 原来在这里就已经开始要将数据缓存了
func WAPI_AcceptGDSFare(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var err error
	defer func() {
		if err != nil {
			fmt.Fprint(w, errorlog.Result(false))
		} else {
			fmt.Fprint(w, errorlog.Result(true))
		}
	}()

	r.ParseForm()
	WebData, _ := ioutil.ReadAll(r.Body)

	if stopAccept { //在处理过期
		return
	}

	gnr, err := gzip.NewReader(bytes.NewBuffer(WebData))
	if err != nil {
		errorlog.WriteErrorLog("WAPI_AcceptGDSFare (1): " + err.Error())
		return
	}
	result, _ := ioutil.ReadAll(gnr)
	gnr.Close()

	var fares mysqlop.MutilDaysMainBill
	if err = json.Unmarshal(result, &fares); err != nil {
		return
	}

	if len(fares.Fares) == 0 || len(fares.Fares[0][0]) == 0 {
		return
	}

	if fares.Fares[0][0][0].Trip == 1 && len(fares.Fares[0][0]) != len(fares.Fares[0][1]) {
		return
	}

	if fares.Fares[0][0][0].Trip == 1 && fares.Fares[0][0][0].Springboard == "CAN" && fares.Fares[0][0][0].Destination == "BKK" {
		fmt.Println(errorlog.DateTime(), "CAN-BKK", fares.Fares[0][0][0].TravelFirstDate, fares.Fares[0][1][0].TravelFirstDate, fares.Fares[0][0][0].Agency, fares.Fares[0][0][0].PCC, len(fares.Fares[0][0]), len(fares.Fares[0][1]))
	}

	go func() {
		var depart, arrive, traveldate string //traveldate去程的回程日期,回程的去程日期

		for _, dayfare := range fares.Fares {
			for GoorBack, fare := range dayfare {

				if len(dayfare) == 2 && fares.Fares[0][0][0].Trip == 1 {
					if GoorBack == 0 && len(fares.Fares[0][1]) > 0 {
						traveldate = fares.Fares[0][1][0].TravelFirstDate
					} else if GoorBack == 1 {
						traveldate = fares.Fares[0][0][0].TravelFirstDate
					}
				}

				for _, mb := range fare {
					mb.GoorBack = GoorBack
					mb.ApplyHumen = "A"
					mb.ID = getID()
					//mb.BillBerth = Cabin[mb.AirInc+mb.Berth]//这里的mb.Berth是多个的
					//if mb.BillBerth == "" {
					mb.BillBerth = "Y"
					//}
					mb.TravelLastDate = traveldate //查询条件
					if mb.TransferCity == "" || mb.Springboard == mb.TransferCity || mb.TransferCity == mb.Destination {
						mb.Routine = mb.Springboard + "-" + mb.FirstTransfer + "-" + mb.Destination
					} else {
						mb.Routine = mb.Springboard + "-" + mb.FirstTransfer + "-" + mb.TransferCity + "-" + mb.SecondTransfer + "-" + mb.Destination
					}

					if depart != mb.Springboard ||
						arrive != mb.Destination {

						depart = mb.Springboard
						arrive = mb.Destination
						tmb := &mysqlop.MainBill{
							TravelFirstDate: mb.TravelFirstDate,
							TravelLastDate:  traveldate,
							Agency:          mb.Agency,
							PCC:             mb.PCC,
							Trip:            mb.Trip,
							GoorBack:        GoorBack,
							Routine:         "Overdue",
						}
						b2fareCache(&QueryDepart, mb.Springboard, mb.Destination, tmb, true)
					}
					b2fareCache(&QueryDepart, mb.Springboard, mb.Destination, mb, true)
				}
			}
		}
	}()
}

//查询某命令发布后的票单数据
func WAPI_CommandPulishedFare(w http.ResponseWriter, r *http.Request) {

	defer errorlog.DealRecoverLog()

	type PublishedFare struct {
		CommandID    int    `json:"CommandID"`
		AirInc       string `json:"MarkingAirline"`
		Springboard  string `json:"DepartStation"`
		TransferCity string `json:"ConnectStation"`
		Destination  string `json:"ArriveStation"`
		FareBase     string `json:"FareBase"`
		Berth        string `json:"BookingCode"`
		BillBerth    string `json:"Cabin"`

		AdultsPrice    int    `json:"AdultsPrice"`
		ChildrenPrice  int    `json:"ChildrenPrice"`
		Trip           int    `json:"Trip"`
		GoorBack       int    `json:"GoorBack"`
		ApplyAir       string `json:"ApplyFlight"`
		NotFitApplyAir string `json:"NotApplyFlight"`

		TravelFirstDate  string `json:"FirstTravelDate"`
		TravelLastDate   string `json:"LastTravelDate"`
		ReserveFirstDate string `json:"FirstSaleDate"`
		ReserveLastDate  string `json:"LastSaleDate"`
		MinStay          int    `json:"MinStay"`
		MaxStay          int    `json:"MaxStay"`
		WeekFirst        int    `json:"WeekFirst"`
		WeekLast         int    `json:"WeekLast"`
		OutBill2         int    `json:"AdvpResepvations"`

		Routine    string `json:"Routine"`
		PriceOrder string `json:"PriceOrder"`
		Remark     string `json:"Remark"`
		Agency     string `json:"GDS"`
		PCC        string `json:"PCC"`
	}

	type ParamData struct {
		CommandID int `json:"CommandID"`
	}

	var err error
	PFs := make([]*PublishedFare, 0, 30)
	defer func() {
		fmt.Fprint(w, string(errorlog.Make_JSON_GZip(PFs)))
	}()

	r.ParseForm()
	WebData, _ := ioutil.ReadAll(r.Body)

	var QueryParam ParamData
	if err = json.Unmarshal(WebData, &QueryParam); err != nil {
		errorlog.WriteErrorLog("WAPI_CommandPulishedFare (1): " + err.Error())
		return
	}

	DepartArrive, err := GDSCommandDepartArrive(QueryParam.CommandID)

	if err == nil {
		for _, da := range DepartArrive {
			QueryDepart.mutex.RLock()
			departother, ok := QueryDepart.DepartStation[da[0]]
			QueryDepart.mutex.RUnlock()

			if !ok {
				continue
			}

			departother.mutex.RLock()
			datesfares, ok := departother.ArriveStation[da[1]]
			departother.mutex.RUnlock()

			if !ok || len(datesfares.b2fare) == 0 {
				continue
			}

			for _, fare := range datesfares.b2fare {
				if fare.CommandID == QueryParam.CommandID {
					PFs = append(PFs, &PublishedFare{
						CommandID:    fare.CommandID,
						AirInc:       fare.AirInc,
						Springboard:  fare.Springboard,
						TransferCity: fare.TransferCity,
						Destination:  fare.Destination,
						FareBase:     fare.FareBase,
						Berth:        fare.Berth,
						BillBerth:    fare.BillBerth,

						AdultsPrice:    fare.AdultsPrice,
						ChildrenPrice:  fare.ChildrenPrice,
						Trip:           fare.Trip,
						GoorBack:       fare.GoorBack,
						ApplyAir:       fare.ApplyAir,
						NotFitApplyAir: fare.NotFitApplyAir,

						TravelFirstDate:  fare.TravelFirstDate,
						TravelLastDate:   fare.TravelLastDate,
						ReserveFirstDate: fare.ReserveFirstDate,
						ReserveLastDate:  fare.ReserveLastDate,
						MinStay:          fare.MinStay,
						MaxStay:          fare.MaxStay,
						WeekFirst:        fare.WeekFirst,
						WeekLast:         fare.WeekLast,
						OutBill2:         fare.OutBill2,

						Routine:    fare.Routine,
						PriceOrder: fare.PriceOrder,
						Remark:     fare.Remark,
						Agency:     fare.Agency,
						PCC:        fare.PCC})
				}
			}
		}
	} else {
		errorlog.WriteErrorLog("WAPI_CommandPulishedFare (2): " + err.Error())
	}

}


//接口----- 删除一条航线的条款
func WAPI_RuleConditionDeleteLine(w http.ResponseWriter, r *http.Request) {

	defer errorlog.DealRecoverLog()

	errorlog.WriteErrorLog("WAPI_RuleConditionDeleteLine (1): ")

	var err error

	//延迟函数。遇到返回后的东西有问题，则可以通过这么搞，将err返回出去
	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}
		body, _ := json.Marshal(struct {
			ErrorStr string `json:"ErrorStr"`
		}{
			ErrorStr: errstr})

		fmt.Fprint(w, string(body))
	}()

	r.ParseForm()

	//请求的参数。result为byte类型数据
	result, _ := ioutil.ReadAll(r.Body)

	var QueryParam struct {
		DepartStation string `json:"DepartStation"` //出发机场
		ArriveStation string `json:"ArriveStation"` //到达机场
		Airline       string `json:"Airline"`       //航司
	}

	if err = json.Unmarshal(result, &QueryParam); err != nil {
		return
	}

	if len(QueryParam.DepartStation) != 3 || len(QueryParam.ArriveStation) != 3 || len(QueryParam.Airline) != 2 {
		err = errors.New("Param Error")
		return
	}

	err = RuleConditionDeleteLine(QueryParam.DepartStation, QueryParam.ArriveStation, QueryParam.Airline)
}




///////////////////#TODO Fare包中  已经弄懂的
//条件查询GDSFare列表。通过输入的参数转成sql语句查询对应的FareList（最终实质上是与数据库交互）
func WAPI_GDSFareQuery(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	errorlog.WriteErrorLog("WAPI_GDSFareQuery (1): ")

	var err error

	type Obj struct {
		CommandID       int    `json:"CommandID"`
		FareType        int    `json:"FareType"`
		Departure       string `json:"Departure"`
		Arrival         string `json:"Arrival"`
		Airline         string `json:"Airline"`
		TripType        string `json:"TripType"`
		Currency        string `json:"Currency"`
		CommandStr      string `json:"CommandStr"`
		IndexID         int    `json:"Index"`
		TravelFirstDate string `json:"TravelFirstDate"`
		TravelLastDate  string `json:"TravelLastDate"`
		BookingClass    string `json:"BookingClass"`
		Cabin           string `json:"Cabin"`
		FareBase        string `json:"FareBase"`
		WeekLimit       string `json:"WeekLimit"`
		MinStay         int    `json:"MinStay"`
		MaxStay         int    `json:"MaxStay"`
		ApplyAir        string `json:"ApplyAir"`
		NotFitApplyAir  string `json:"NotFitApplyAir"`
		AdultPrice      int    `json:"AdultPrice"`
		ChildPrice      int    `json:"ChildPrice"`
		GDS             string `json:"GDS"`
		Agency          string `json:"Agency"`
		PriceOrder      string `json:"PriceOrder"`
		ApplyRoutine    string `json:"ApplyRoutine"`
		TravelDate      string `json:"TravelDate"`
		BackDate        string `json:"BackDate"`
		NotTravel       string `json:"NotTravel"`
		AdvpTicketing    string `json:"AdvpTicketing"`
		AdvpResepvations int    `json:"AdvpResepvations"`
		Remark           string `json:"Remark"`
		FirstSalesDate   string `json:"FirstSalesDate"`
		LastSalesDate    string `json:"LastSalesDate"`
		NumberOfPeople   int    `json:"NumberOfPeople"`
		Status int `json:"Status"`

	}
	var obj []*Obj

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}

		fmt.Fprint(w, errorlog.Make_JSON_GZip_Reader(struct {
			Obj      interface{} `json:"Obj"`
			ErrorStr string      `json:"ErrorStr"`
		}{
			Obj:      obj,
			ErrorStr: errstr}))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)



	//Departure和Arrival 可能为二位，也可能为三位  TripType代表单程还是往返
	var QueryParam struct {
		Airline    []string `json:"Airline"`  //这里的航司是传数组
		Departure  string   `json:"Departure"`
		Arrival    string   `json:"Arrival"`
		TripType   string   `json:"TripType"`
		TravelDate string   `json:"TravelDate"`
	}

	if err = json.Unmarshal(result, &QueryParam); err != nil {
		return
	}

	sqlWhere := "Where"


	//接下来这些判断主要是为了拿出出发地和目的地的一些塞选
	//出发地为三字代码，也就是只有一个城市的时候
	if len(QueryParam.Departure) == 3 {
		sqlWhere += " Departure='" + QueryParam.Departure + "'"
	} else if len(QueryParam.Departure) == 2 {
		//出发地为两字代码，则可能传入的是一个国家。国家是二字代码
		var citys []string
		var ok bool
		if citys, ok = cachestation.CountryCity[QueryParam.Departure]; !ok {
			err = errors.New("Invalid Departure")
			return
		}

		sqlWhere += " Departure in ("
		for i, city := range citys {

			//国家列表里面有多个城市。
			//如果是第一个城市的话
			if i == 0 {
				sqlWhere += "'" + city + "'"
			} else {
				//其他城市
				sqlWhere += ",'" + city + "'"
			}
		}
		sqlWhere += ")"
	} else {

		//没有出发地
		err = errors.New("No Departure")
		return
	}


	if len(QueryParam.Arrival) == 3 {

		sqlWhere += " And Arrival='" + QueryParam.Arrival + "'"
	} else if len(QueryParam.Arrival) == 2 {
		var citys []string
		var ok bool
		if citys, ok = cachestation.CountryCity[QueryParam.Arrival]; !ok {
			err = errors.New("Invalid Arrival")
			return
		}
		sqlWhere = " And Arrival in ("
		for i, city := range citys {
			if i == 0 {
				sqlWhere += "'" + city + "'"
			} else {
				sqlWhere += ",'" + city + "'"
			}
		}
		sqlWhere += ")"
	} else {
		err = errors.New("No Arrival")
		return
	}


	//上面两步已经将出发地和目的地处理好，同时也兼容了出发地，目的地，是国家或者城市的情况。
	//里面catheflight，以及for循环,可以将条件放广，一定程度上加大了查询的范围



	//这里将航司也一起整理进去了
	if len(QueryParam.Airline) == 1 {
		sqlWhere += " And Airline='" + QueryParam.Airline[0] + "'"
	} else if len(QueryParam.Airline) > 1 {
		sqlWhere = " And Airline in ("
		for i, Airline := range QueryParam.Airline {
			if i == 0 {
				sqlWhere += "'" + Airline + "'"
			} else {
				sqlWhere += ",'" + Airline + "'"
			}
		}
		sqlWhere += ")"
	}

	//旅程情况，判断是单程还是往返。如果传入的是OW或者RT则证明输入是合法的。
	if QueryParam.TripType == "OW" || QueryParam.TripType == "RT" {
		sqlWhere += " And TripType='" + QueryParam.TripType + "'"
	}



	//日期的长度必须必须为10位。接着旅行的日期，必须在TravelFirstDate之后，也必须在TravelLastDate之前
	if len(QueryParam.TravelDate) == 10 {
		sqlWhere += " And TravelFirstDate<='" + QueryParam.TravelDate + "' And TravelLastDate>='" + QueryParam.TravelDate + "'"
	}
	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return
	}
	defer conn.Close()

	sqlSelect := `Select CommandID,FareType,Departure,Arrival, Airline,TripType,Currency,
		CommandStr,IndexID,TravelFirstDate,TravelLastDate,BookingClass,Cabin,FareBase,
		WeekLimit,MinStay,MaxStay,ApplyAir,NotFitApplyAir,AdultPrice,ChildPrice,GDS,Agency,
		PriceOrder,ApplyRoutine,TravelDate,BackDate,NotTravel,AdvpTicketing,AdvpResepvations,
		RemarkText,FirstSalesDate,LastSalesDate,NumberOfPeople,Status
	From GDSFare ` + sqlWhere

	row, b := mysqlop.MyQuery(conn, sqlSelect)
	if !b {
		err = errors.New("Query Error")
		return
	}
	defer row.Close()

	for row.Next() {
		o := Obj{}
		if err = row.Scan(&o.CommandID, &o.FareType, &o.Departure, &o.Arrival, &o.Airline, &o.TripType, &o.Currency,
			&o.CommandStr, &o.IndexID, &o.TravelFirstDate, &o.TravelLastDate, &o.BookingClass, &o.Cabin, &o.FareBase,
			&o.WeekLimit, &o.MinStay, &o.MaxStay, &o.ApplyAir, &o.NotFitApplyAir, &o.AdultPrice, &o.ChildPrice, &o.GDS, &o.Agency,
			&o.PriceOrder, &o.ApplyRoutine, &o.TravelDate, &o.BackDate, &o.NotTravel, &o.AdvpTicketing, &o.AdvpResepvations,
			&o.Remark, &o.FirstSalesDate, &o.LastSalesDate, &o.NumberOfPeople, &o.Status); err == nil {
			obj = append(obj, &o)
		}
	}
}


//CommandID查询FDSFare列表。同时返回票价跟条款的一些内容（其实和上面的数据有点类似。只不过这里接受的是传入命令.查询了之后发现同一个commandID会返回好多条一样的数据）
func WAPI_GDSFareList(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	errorlog.WriteErrorLog("WAPI_GDSFareList (1): ")

	var err error

	type Obj struct {
		CommandID       int    `json:"CommandID"`  //命令ID
		FareType        int    `json:"FareType"`  //基本都是0。这里的FareType到达代表什么默认为直达；有直达和中转两个选项；(应该来说直达代表为0，而中转代表为1)
		Departure       string `json:"Departure"`  //出发
		Arrival         string `json:"Arrival"`  //到达
		Airline         string `json:"Airline"`  //航司
		TripType        string `json:"TripType"`   //单程or 往返  (RT  or OW// )
		Currency        string `json:"Currency"`   //币种
		CommandStr      string `json:"CommandStr"`   //字符串拼接的 FSD ZIYSIN/20JUN18/CX<ADT/*RT/X/*CNY
		IndexID         int    `json:"Index"`   //索引
		TravelFirstDate string `json:"TravelFirstDate"`   //旅行首天
		TravelLastDate  string `json:"TravelLastDate"`    //旅行末天
		BookingClass    string `json:"BookingClass"`  //B/N 这里代表舱位代码吧
		Cabin           string `json:"Cabin"`   //舱位
		FareBase        string `json:"FareBase"`  //NLABRPDA 这个其实就是折扣代码了吧
		WeekLimit       string `json:"WeekLimit"`
		MinStay         int    `json:"MinStay"`  //最少几天
		MaxStay         int    `json:"MaxStay"`   //最多几天
		ApplyAir        string `json:"ApplyAir"`   //适合的航司  CX(CX)  和之前丽素讲的关于票单那部分很类似
		NotFitApplyAir  string `json:"NotFitApplyAir"`  //不适合的航司
		AdultPrice      int    `json:"AdultPrice"`  //大人价格
		ChildPrice      int    `json:"ChildPrice"`  //小孩价格
		GDS             string `json:"GDS"`    //1E 代表数据源
		Agency          string `json:"Agency"`  //CAN131 供应商
		PriceOrder      string `json:"PriceOrder"`  //true
		ApplyRoutine    string `json:"ApplyRoutine"`    //如 ZIY-CX 3A KA-HKG-CX KA-SIN
		TravelDate      string `json:"TravelDate"`   //21FEB18-30APR18 05OCT17-14FEB18 24SEP17-29SEP17 旅行出发日期
		BackDate        string `json:"BackDate"`  //旅行回程日期
		NotTravel       string `json:"NotTravel"`  //不能适用的日期  15FEB18-20FEB18 30SEP17-04OCT17
		AdvpTicketing    string `json:"AdvpTicketing"`
		AdvpResepvations int    `json:"AdvpResepvations"`
		Remark           string `json:"Remark"`  //备注吧
		FirstSalesDate   string `json:"FirstSalesDate"`  //首次销售日期
		LastSalesDate    string `json:"LastSalesDate"`  //最后销售日期
		NumberOfPeople   int    `json:"NumberOfPeople"`  //人数
		Status int `json:"Status"`
	}
	var obj []*Obj

	defer func() {
		errstr := ""
		if err != nil {
			errstr = err.Error()
		}

		fmt.Fprint(w, errorlog.Make_JSON_GZip_Reader(struct {
			Obj      interface{} `json:"Obj"`
			ErrorStr string      `json:"ErrorStr"`
		}{
			Obj:      obj,
			ErrorStr: errstr}))
	}()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var CID struct {
		CommandID int `json:"CommandID"`  //id
	}

	if err = json.Unmarshal(result, &CID); err != nil || CID.CommandID == 0 {
		return
	}

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return
	}
	defer conn.Close()

	sqlSelect := `Select CommandID,FareType,Departure,Arrival, Airline,TripType,Currency,
		CommandStr,IndexID,TravelFirstDate,TravelLastDate,BookingClass,Cabin,FareBase,
		WeekLimit,MinStay,MaxStay,ApplyAir,NotFitApplyAir,AdultPrice,ChildPrice,GDS,Agency,
		PriceOrder,ApplyRoutine,TravelDate,BackDate,NotTravel,AdvpTicketing,AdvpResepvations,
		RemarkText,FirstSalesDate,LastSalesDate,NumberOfPeople,Status
	From GDSFare Where CommandID=?`

	row, b := mysqlop.MyQuery(conn, sqlSelect, CID.CommandID)
	if !b {
		err = errors.New("Query Error")
		return
	}
	defer row.Close()

	for row.Next() {
		o := Obj{}
		if err = row.Scan(&o.CommandID, &o.FareType, &o.Departure, &o.Arrival, &o.Airline, &o.TripType, &o.Currency,
			&o.CommandStr, &o.IndexID, &o.TravelFirstDate, &o.TravelLastDate, &o.BookingClass, &o.Cabin, &o.FareBase,
			&o.WeekLimit, &o.MinStay, &o.MaxStay, &o.ApplyAir, &o.NotFitApplyAir, &o.AdultPrice, &o.ChildPrice, &o.GDS, &o.Agency,
			&o.PriceOrder, &o.ApplyRoutine, &o.TravelDate, &o.BackDate, &o.NotTravel, &o.AdvpTicketing, &o.AdvpResepvations,
			&o.Remark, &o.FirstSalesDate, &o.LastSalesDate, &o.NumberOfPeople, &o.Status); err == nil {
			obj = append(obj, &o)
		}
	}
}
