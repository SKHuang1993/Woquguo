package fare

import (
	"cachestation"
	"database/sql"
	"errorlog"
	"errors"
	"fmt"
	"mysqlop"
	"outsideapi"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)


//Fare获取中,前端提交的处理命令.
type MessQueue struct {
	ID         int        `json:"ID"`         //命令行编号,用于删除命令,修改命令
	Command    string     `json:"CommandStr"` //命令行(查询命令是不会进入队列的)
	Status     int        `json:"Status"`     //状态(0)Insert (1)Doing (2)Other (3)Done
	Operator   string     `json:"Operator"`   //操作人
	InsertTime string     `json:"InsertTime"` //时间
	before     *MessQueue `json:"-"` //将前一条命令和后一条命令都存起来，后面用-表示忽略掉这条消息//有点像链表
	next       *MessQueue `json:"-"`
}



//类似于sort那个包，为了方便互换等
//将命令查询的结构都存起来

//ListMessQueue 是一个MessQueue数组。将各个Queue都存起来
//#TODO 升哥很多地方喜欢定义一个变量，里面存了很多model。接着用Len（）   Swap（） Less（）来处理
type ListMessQueue []*MessQueue

func (this ListMessQueue) Len() int { return len(this) }

func (this ListMessQueue) Swap(i, j int) { this[i], this[j] = this[j], this[i] }

//将MessQueue按照命令从小到大排上去
func (this ListMessQueue) Less(i, j int) bool { return this[i].ID < this[j].ID }



//Fare获取命令队列.(这个是缓存体)  这是一个队列。因此队列有head，有tail
type CommandMission struct {

	head  *MessQueue
	tail  *MessQueue
	count int //现有多少命令未处理
	mutex sync.RWMutex  //整了一个锁进去。可以加锁，可以解锁
	exec  []*CommMissionExec //对head队列进行处理的线程
}


//命令处理进程,负责循环获取命令制作Fare.（这个时被执行的调用体）
type CommMissionExec struct {
	CM   *CommandMission
	exit chan int //退出(非系统中断退出,是线程退出)
	stop chan int //停止该命令获取,可能前端要求停止获取
}

//把命令信息插入队列.
func (this *CommandMission) Insert(comm *MessQueue) error {
	this.mutex.Lock()

	//未处理的命令+1
	this.count++

	if this.head == nil {
		this.head, this.tail = comm, comm
	} else {
		//链表的处理
		this.tail.next = comm
		comm.before = this.tail
		this.tail = comm
	}
	//这里应该是否启动CommMissionExec,或者直接执行命令
	this.mutex.Unlock()

	return nil
}

//从命令队列中删除信息.（传入的comm有一个ID，当遍历的时候这个ID遇到相同的时候，就要减少）
func (this *CommandMission) Delete(comm *MessQueue) error {

	this.mutex.Lock()

	//类似指针的移动，一步步往下走
	for head := this.head; head != nil; head = head.next {
		if head.ID == comm.ID {
			this.count--
			if head.before == nil {
				this.head = head.next
				if this.head != nil {
					this.head.before = nil
				}
			} else {
				head.before.next = head.next
				if head.next != nil {
					head.next.before = head.before
				}
			}
			break
		}
	}
	this.mutex.Unlock()

	return nil
}

//更新命令的内容.(这个处理暂时不使用)
func (this *CommandMission) Update(comm *MessQueue) error {

	this.mutex.Lock()

	for head := this.head; head != nil; head = head.next {
		if head.ID == comm.ID {
			head.Command = comm.Command
			//head.InsertTime = comm.InsertTime
			break
		}
	}
	this.mutex.Unlock()

	return nil
}


//CommMissionExec使用的,循环提取命令的函数.
// 这里要把取出的命令放入到处理队列中去.

func (this *CommandMission) Get() (*MessQueue, error) {
	this.mutex.Lock()
	head := this.head
	if this.head != nil {
		this.count--
		if this.head.next != nil {
			this.head.next.before = nil
		}
		this.head = this.head.next
	}
	this.mutex.Unlock()

	if head == nil {
		return head, errors.New("CommandMission No Order")
	} else {
		head.Status = 1
		doingCommand.mutex.Lock()
		doingCommand.queue[head.ID] = head
		doingCommand.mutex.Unlock()
		CommandUpdateID(head.ID, 1)
		return head, nil
	}
}





//启动一个CommMissionExec
func (this *CommandMission) Exec() (*CommMissionExec, error) {
	exec := &CommMissionExec{
		CM:   this,
		exit: make(chan int, 1),
		stop: make(chan int, 1)}

	this.mutex.Lock()
	this.exec = append(this.exec, exec)
	this.mutex.Unlock()

	go exec.Daemon()

	return exec, nil
}



//CommMissionExec守护进程
func (this *CommMissionExec) Daemon() {


	var (
		mq             *MessQueue
		err            error
		commandStr     string
		departStations CSList
		arriveStations CSList
		Airlines       []string
		TravelDate     string
		RouteType      string
		RTs            []string
		IsNego         string
		AccountCode    string

		o_mq *MessQueue
	)

ReDo:
	select {
	case <-this.exit:
		goto Exit
	default:
		mq, err = this.CM.Get()
	}

	if err != nil {
		time.Sleep(time.Second * 30)
		goto ReDo
	}

	commandStr = strings.Split(mq.Command, " ")[0]

	if commandStr != "update" && commandStr != "get" {
		CommandUpdateID(mq.ID, 2)
		mq.Status = 2
		doingCommand.mutex.Lock()
		delete(doingCommand.queue, mq.ID)
		doingCommand.mutex.Unlock()
		goto ReDo
	}

	//这里应该处理先前'中断'获取留下的数据.

	o_mq = mq
	CommandUpdateID(mq.ID, 1)
	//o_mq.Status = 1 这里在CM.get()已经完成

	if commandStr == "get" {
		commandStr, departStations, arriveStations, Airlines, TravelDate, RouteType, IsNego, AccountCode, err = CommandParse(mq.Command, true)

	} else if commandStr == "update" {
		if mq, err = CommandSuper(mq.ID); err != nil {
			//错误日志记录
			CommandUpdateID(o_mq.ID, 2)
			o_mq.Status = 2
			doingCommand.mutex.Lock()
			delete(doingCommand.queue, o_mq.ID)
			doingCommand.mutex.Unlock()
			goto ReDo
		}
		commandStr, departStations, arriveStations, Airlines, TravelDate, RouteType, IsNego, AccountCode, err = CommandParse(mq.Command, true)
		GdsFareDelete(mq.ID, departStations, arriveStations)
	}

	if RouteType == "" {
		RTs = []string{"RT", "OW"}
	} else {
		RTs = []string{RouteType}
	}

	for _, depart := range departStations {
		for _, arrive := range arriveStations {
			for _, airline := range Airlines {
				for _, rt := range RTs {
					select {
					case <-this.exit:
						//中断的操作,以后重启后按Update重做
						goto Exit
					case <-this.stop:
						//停止获取的清理工作
						goto ReDo
					default:
						var das, aas []string
						var ok bool
						if das, ok = cachestation.CityCounty[depart.Airport]; !ok {
							das = []string{depart.Airport} //在指定机场的情况下直接使用机场数据
						}

						if aas, ok = cachestation.CityCounty[arrive.Airport]; !ok {
							aas = []string{arrive.Airport} //在指定机场的情况下直接使用机场数据
						}

						for _, da := range das { //制作票单时必须把城市变机场取查询
							for _, aa := range aas {
								if err := this.MakeFare(da, aa, airline, TravelDate, rt, "ADT", mq.ID, "CAN131", "true", IsNego, AccountCode); err != nil {
									fmt.Println(errorlog.DateTime(), "MakeFare ==>"+err.Error())
									goto BillNext
								}
							}
						}
					}
				BillNext:
				}
			}
		}
	}

	//完成命令
	CommandUpdateID(o_mq.ID, 3)
	o_mq.Status = 3
	doingCommand.mutex.Lock()
	delete(doingCommand.queue, o_mq.ID)
	doingCommand.mutex.Unlock()

	goto ReDo

Exit:
}



//下面的处理函数归类到CommMissionExec主要是想以后使用错误信息队列
func (this *CommMissionExec) MakeFare(
	depart string,
	arrive string,
	Airline string,
	TravelDate string,
	RouteType string, //OW RT
	TravelerType string, //ADT CHD
	CommandID int,
	Agency string, //PCC字段
	PriceOrder string,
	IsNego string,
	AccountCode string) error {

	fmt.Println(errorlog.DateTime(), "MakeFare", depart+"-"+Airline+"-"+arrive, RouteType, TravelDate)

	var retErr error
	chanFR := make(chan *outsideapi.FareResponse, 1)
	outsideapi.SearchXSFSD(depart, arrive, Airline, TravelDate, RouteType, TravelerType, PriceOrder, IsNego, AccountCode, chanFR)
	fr := <-chanFR
	for ci := 0; ci < 3; ci++ {
		if fr == nil || len(fr.XsfsdList) == 0 { //网络问题再做多一次
			errorlog.WriteErrorLog("MakeFare (0): SearchXSFSD List=0, Error Count " + strconv.Itoa(ci+1))
			outsideapi.SearchXSFSD(depart, arrive, Airline, TravelDate, RouteType, TravelerType, PriceOrder, IsNego, AccountCode, chanFR)
			fr = <-chanFR
		} else {
			break
		}
	}
	oneMonthLate := time.Now().AddDate(0, 1, 0).Format("2006-01-02")

	var nonDone = []struct {
		gdsfare *outsideapi.GDSFare
		rule    *Rule
	}{}
	var hroutine = make(map[string]int, 20)
	var listGDSFare = make(outsideapi.ListGDSFare, 0, 500)
	var listRule = make([]*Rule, 0, 300)

	for _, farecmd := range fr.XsfsdList {
		farecmd.Init(CommandID, Agency, PriceOrder, TravelDate) //这里把FareCmdInfo反填入GDSFare.

		for _, gdsfare := range farecmd.Fares {

			if /*gdsfare.Departure != depart || gdsfare.Arrival != arrive ||*/ gdsfare.Airline != Airline {
				errorlog.WriteErrorLog("命令ID: " + strconv.Itoa(CommandID) + " 跳过记录: " + gdsfare.Departure + "-" + gdsfare.Airline + "-" + gdsfare.Arrival)
				continue //depart,arrive可能是城市,gdsfare.Departure,gdsfare.Arrival是机场
			}

			if gdsfare.Departure != depart || gdsfare.Arrival != arrive {
				if retErr == nil {
					retErr = errors.New("City Data " + gdsfare.Departure + " To " + gdsfare.Arrival)
				}
			}

			/****处理条款流程****/
			chanSN := make(chan string, 1)
			sn := ""
			sni := 0
			rule := &Rule{}
			id := gdsfare.Departure + gdsfare.Arrival + gdsfare.Airline + strings.Replace(gdsfare.FareBase, "/", "_", -1)
			var ok bool

			QueryIndex := strconv.Itoa(gdsfare.Index)
			if len(QueryIndex) == 1 {
				QueryIndex = "0" + QueryIndex
			}

			RuleCondition.mutex.RLock()
			rule.RuleRecord, ok = RuleCondition.Condition[id]
			RuleCondition.mutex.RUnlock()

			if !ok || rule.InsertDate > oneMonthLate {

				for ; sni < 5; sni++ {
					outsideapi.SearchXSFSN(gdsfare.CommandStr, QueryIndex, chanSN)
					sn = <-chanSN
					if len(sn) > 0 {
						break
					}
				}

				if sni >= 5 {
					gdsfare.Status = 1
					listGDSFare = append(listGDSFare, gdsfare)
					errorlog.WriteErrorLog("MakeFare (1) (SN失败5次): " + gdsfare.CommandStr + " " + QueryIndex)
					continue //没办法完成的舱位
				}

				if err := rule.Init(sn, Airline); err != nil {
					gdsfare.Status = 2
					listGDSFare = append(listGDSFare, gdsfare)
					errorlog.WriteErrorLog("MakeFare (2): " + err.Error())
					continue //没办法完成的舱位
				}

				rule.RuleParse()
				rule.ID = id
				errorlog.ContextWrite2File(sn, "/home/wds/KSFare/"+rule.ID+".sn")

				//RuleConditionInsert(rule.RuleRecord) //要先检查条款是否要获取等
				listRule = append(listRule, rule)
				RuleCondition.mutex.Lock()
				RuleCondition.Condition[rule.ID] = rule.RuleRecord
				RuleCondition.mutex.Unlock()

			}

			/****处理适合航班问题****/
			chanSL := make(chan *outsideapi.XSFSLresponse, 1)
			outsideapi.SearchXSFSL(gdsfare.CommandStr, QueryIndex, chanSL)
			sl := <-chanSL

			if len(sl.Content) > 0 { //这里不一定可以获取到航线
				gdsfare.ApplyRoutine = ApplyRoutine(sl.Content, gdsfare.Departure, gdsfare.Arrival, gdsfare.Airline)
				errorlog.ContextWrite2File("Index="+QueryIndex+" (wds)\n"+sl.Content, "/home/wds/KSFare/"+rule.ID+".sl")
			}

			//使用舱位
			if len(gdsfare.ApplyRoutine) > 0 {
				for _, routines := range strings.Split(gdsfare.ApplyRoutine, "$") {
					if strings.Count(routines, "-") == 4 { //中转才需要获取舱位
						route := strings.Split(routines, "-")
						for _, first := range strings.Split(route[1], " ") {
							for _, second := range strings.Split(route[3], " ") {
								chanXS := make(chan *outsideapi.XSFXSResponse, 1)
								rtmp := route[0] + "-" + first + "-" + route[2] + "-" + second + "-" + route[4]
								outsideapi.SearchXSFXS(gdsfare.CommandStr, QueryIndex, rtmp, chanXS)
								xs := <-chanXS

								if len(xs.ResultInfo) > 0 {
									if cabin := ApplyCabin(xs.ResultInfo); len(cabin) > 0 {
										gdsfare.BookingClass = cabin
										errorlog.ContextWrite2File("Index="+QueryIndex+"  Routine="+rtmp+" (wds)\n"+xs.ResultInfo, "/home/wds/KSFare/"+rule.ID+".xs")
										goto Next
									}
								}
							}
						}
					}
				}
			} else { //这里处理GDS航线问题
				var departs, arrives []string
				ok := false
				if retErr != nil {
					if departs, ok = cachestation.CityCounty[gdsfare.Departure]; !ok {
						departs = []string{gdsfare.Departure}
					}
					if arrives, ok = cachestation.CityCounty[gdsfare.Arrival]; !ok {
						arrives = []string{gdsfare.Arrival}
					}
					for _, depart := range departs {
						for _, arrive := range arrives {
							routine := depart + "-" + gdsfare.Airline + "-" + arrive
							if _, ok = cachestation.DirectRoutine[routine]; ok {
								gdsfare.ApplyRoutine = routine
								goto Next
							}
						}
					}
				} else {
					routine := gdsfare.Departure + "-" + gdsfare.Airline + "-" + gdsfare.Arrival
					if _, ok := cachestation.DirectRoutine[routine]; ok {
						gdsfare.ApplyRoutine = routine
					} else {
						nonDone = append(nonDone, struct {
							gdsfare *outsideapi.GDSFare
							rule    *Rule
						}{
							gdsfare: gdsfare,
							rule:    rule,
						})

						continue
					}
				}
			}

		Next:
			gdsfare.GDS = "1E"
			gdsfare.TravelDate = rule.TravelDate
			gdsfare.BackDate = rule.BackDate
			gdsfare.NotTravel = rule.NotTravel
			gdsfare.ApplyAir = rule.FlightApplication
			gdsfare.NotFitApplyAir = rule.FlightNoApplication
			gdsfare.Cabin = Cabin[gdsfare.Airline+gdsfare.BookingClass]
			if gdsfare.Cabin == "" {
				gdsfare.Cabin = "Y"
			}
			gdsfare.AdvpResepvations = rule.OutBill2
			gdsfare.FirstSalesDate = rule.FirstSalesDate
			gdsfare.LastSalesDate = rule.LastSalesDate
			gdsfare.IsRt = rule.IsRt
			gdsfare.NumberOfPeople = rule.NumberOfPeople

			for _, hr := range strings.Split(gdsfare.ApplyRoutine, "$") { //rule在这里已经失败了.因为不同IndexID获取是不同的Routine
				hroutine[hr]++
			}

			if gdsfare.MinStay == 0 {
				gdsfare.MinStay = rule.MinStay
			}
			if gdsfare.MaxStay == 360 {
				gdsfare.MaxStay = rule.MaxStay
			}

			if len(gdsfare.BookingClass) == 2 { //J* F* Y*
				gdsfare.BookingClass = gdsfare.BookingClass[:1]
			}

			gdsfare.Status = 3
			listGDSFare = append(listGDSFare, gdsfare)
		}
	}

	srout := ""
	tc := ""
	tc_get := false
	max := 0
	for k, v := range hroutine {

		if v > max {
			srout = k
			max = v
		} else if v == max {
			srout += "$" + k
		}
	}

	for _, farerule := range nonDone {
		gdsfare := farerule.gdsfare
		rule := farerule.rule
		QueryIndex := strconv.Itoa(gdsfare.Index)
		if len(QueryIndex) == 1 {
			QueryIndex = "0" + QueryIndex
		}

		if len(srout) > 0 {
			gdsfare.ApplyRoutine = srout
			for _, routines := range strings.Split(gdsfare.ApplyRoutine, "$") {
				if strings.Count(routines, "-") == 4 {
					route := strings.Split(routines, "-")
					for _, first := range strings.Split(route[1], " ") {
						for _, second := range strings.Split(route[3], " ") {
							chanXS := make(chan *outsideapi.XSFXSResponse, 1)
							rtmp := route[0] + "-" + first + "-" + route[2] + "-" + second + "-" + route[4]
							outsideapi.SearchXSFXS(gdsfare.CommandStr, QueryIndex, rtmp, chanXS)
							xs := <-chanXS

							if len(xs.ResultInfo) > 0 {
								if cabin := ApplyCabin(xs.ResultInfo); len(cabin) > 0 {
									gdsfare.BookingClass = cabin
									errorlog.ContextWrite2File("Index="+QueryIndex+"  Routine="+rtmp+" (wds)\n"+xs.ResultInfo, "/home/wds/KSFare/"+rule.ID+".xs")
									goto Next2
								}
							}
						}
					}
				}
			}
		} else {
			if tc == "" && !tc_get {
				tc = getMinMileConnect(gdsfare.Departure, gdsfare.Arrival)
				tc_get = true
			}

			if len(tc) > 0 {
				ar := gdsfare.Departure + "-" + gdsfare.Airline + "-" + tc + "-" + gdsfare.Airline + "-" + gdsfare.Arrival
				chanXS := make(chan *outsideapi.XSFXSResponse, 1)
				outsideapi.SearchXSFXS(gdsfare.CommandStr, QueryIndex, ar, chanXS)
				xs := <-chanXS

				if len(xs.ResultInfo) > 0 {
					if cabin := ApplyCabin(xs.ResultInfo); len(cabin) > 0 {
						gdsfare.ApplyRoutine = ar
						gdsfare.BookingClass = cabin
						errorlog.ContextWrite2File("Index="+QueryIndex+"  Routine="+ar+" (wds)\n"+xs.ResultInfo, "/home/wds/KSFare/"+rule.ID+".xs")
					}
				}

				if gdsfare.ApplyRoutine == "" {
					gdsfare.Status = 2 //
					listGDSFare = append(listGDSFare, gdsfare)
					errorlog.WriteErrorLog("MakeFare (4): " + gdsfare.Departure + "-" + gdsfare.Airline + "-" + "*" + "-" + gdsfare.Airline + "-" + gdsfare.Arrival)
					continue //没办法完成的舱位
				}
			} else {
				gdsfare.Status = 2 //
				listGDSFare = append(listGDSFare, gdsfare)
				errorlog.WriteErrorLog("MakeFare (5): " + gdsfare.Departure + "-" + gdsfare.Airline + "-" + "*" + "-" + gdsfare.Airline + "-" + gdsfare.Arrival)
				continue //没办法完成的舱位
			}
		}

	Next2:
		gdsfare.GDS = "1E"
		gdsfare.TravelDate = rule.TravelDate
		gdsfare.BackDate = rule.BackDate
		gdsfare.NotTravel = rule.NotTravel
		gdsfare.ApplyAir = rule.FlightApplication
		gdsfare.NotFitApplyAir = rule.FlightNoApplication
		gdsfare.Cabin = Cabin[gdsfare.Airline+gdsfare.BookingClass]
		if gdsfare.Cabin == "" {
			gdsfare.Cabin = "Y"
		}
		gdsfare.AdvpResepvations = rule.OutBill2
		gdsfare.FirstSalesDate = rule.FirstSalesDate
		gdsfare.LastSalesDate = rule.LastSalesDate
		gdsfare.IsRt = rule.IsRt
		gdsfare.NumberOfPeople = rule.NumberOfPeople

		if gdsfare.MinStay == 0 {
			gdsfare.MinStay = rule.MinStay
		}
		if gdsfare.MaxStay == 360 {
			gdsfare.MaxStay = rule.MaxStay
		}

		if len(gdsfare.BookingClass) == 2 { //J* F* Y*
			gdsfare.BookingClass = gdsfare.BookingClass[:1]
		}

		gdsfare.Status = 3
		listGDSFare = append(listGDSFare, gdsfare)
	}

	var conn *sql.DB
	if len(listGDSFare) > 0 || len(listRule) > 0 {
		var err error
		conn, err = mysqlop.LocalConnect()
		if err != nil {
			return err
		}
		defer conn.Close()
	}

	if len(listGDSFare) > 0 {
		sort.Sort(listGDSFare)
		map_fare := make(map[string]int, len(listGDSFare))

		for _, gdsfare := range listGDSFare {

			if gdsfare.Status == 3 {
				if price, ok := map_fare[gdsfare.FareBase+"#"+gdsfare.Cabin+"#"+gdsfare.ApplyRoutine]; !ok || price == gdsfare.AdultPrice {
					map_fare[gdsfare.FareBase+"#"+gdsfare.Cabin+"#"+gdsfare.ApplyRoutine] = gdsfare.AdultPrice
					gdsfare.Status = 4

					//删除内存中相同的Departure,Arrival,Airline,FareBase
					b2fareDelete(&QueryDepart, 5, gdsfare.Airline+gdsfare.FareBase+" "+strconv.Itoa(CommandID), gdsfare.Departure, gdsfare.Arrival)
					b2fareDelete(&QueryArrive, 5, gdsfare.Airline+gdsfare.FareBase+" "+strconv.Itoa(CommandID), gdsfare.Arrival, gdsfare.Departure)

					if AutoPublish {
						gdsfare.Status = 0
						for _, bill := range Copy(gdsfare) {
							b2fareCache(&QueryDepart, bill.Springboard, bill.Destination, bill, false)
							b2fareCache(&QueryArrive, bill.Destination, bill.Springboard, bill, false)
						}
					}
				}
			}

			GDSFareInsert(conn, gdsfare)
		}
	}

	for _, rule := range listRule {
		RuleConditionInsert(conn, rule.RuleRecord)
	}
	return retErr
}
