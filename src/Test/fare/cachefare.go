package fare

import (

	//"bytes"
	"encoding/json"
	"errorlog"
	"errors"
	//"fmt"
	//"io/ioutil"
	"mysqlop"
	//"net/http"
	//"outsideapi"
	"strconv"
	"strings"
	"sync"
	"time"
	"webapi"
)



type OtherStation struct {
	//到达地点
	ArriveStation map[string]*DatesFares  //string==ArriveStation
	mutex         sync.RWMutex
}


type QueryStation struct {
	DepartStation map[string]*OtherStation //string=DepartStation  //出发地
	mutex         sync.RWMutex  //锁
}


type DatesFares struct {
	//FB部分//slip不像map一样产生(concurrent map iteration and map write)错误.
	b2fare mysqlop.ListMainBill //FB FareV2
	mutex2 sync.Mutex
	//GDS部分//DianShang
	gdsfare  map[string][]byte     //mysqlop.ListMainBill //按日期存储,减少像b2fare样式的大数据.
	queue    map[string]*FareQueue //按日期存储队列 //string也就是key，里面存储的应该是对应日期的数据
	mutexgds sync.RWMutex
}



var QueryDepart QueryStation //从出发地开始查询
var QueryArrive QueryStation //从目的地开始查询



/***********Cabin(BillBerth)缓存*****************/
var Cabin map[string]string

/*************是否自动模式************************/
var AutoPublish bool


type FareQueue struct {
	fare *mysqlop.MainBill
	next *FareQueue
}





func (this *DatesFares) Delete(Level int, colText string) error {

	this.mutex2.Lock()

	DSLen := len(this.b2fare)
	switch Level {
	case 0: //删除票单级数据
		BillID := colText
		for i := 0; i < DSLen; {
			if this.b2fare[i].BillID == BillID {
				DSLen--
				this.b2fare[i], this.b2fare[DSLen] = this.b2fare[DSLen], this.b2fare[i]
			} else {
				i++
			}
		}

	case 1: //删除命令级数据
		CommandID, _ := strconv.Atoi(colText)
		for i := 0; i < DSLen; {
			if this.b2fare[i].CommandID == CommandID {
				DSLen--
				this.b2fare[i], this.b2fare[DSLen] = this.b2fare[DSLen], this.b2fare[i]
			} else {
				i++
			}
		}
	case 2: //删除航司级数据
		Airline := colText
		for i := 0; i < DSLen; {
			if this.b2fare[i].AirInc == Airline && this.b2fare[i].BillID == "FareV2" {
				DSLen--
				this.b2fare[i], this.b2fare[DSLen] = this.b2fare[DSLen], this.b2fare[i]
			} else {
				i++
			}
		}
	case 3: //过期
		Overdue := colText
		for i := 0; i < DSLen; {
			if this.b2fare[i].TravelLastDate < Overdue {
				DSLen--
				this.b2fare[i], this.b2fare[DSLen] = this.b2fare[DSLen], this.b2fare[i]
			} else {
				i++
			}
		}
	case 4: //删除所有b2 fare
		for i := 0; i < DSLen; {
			if len(this.b2fare[i].BillID) > 10 {
				DSLen--


				//反转过来
				this.b2fare[i], this.b2fare[DSLen] = this.b2fare[DSLen], this.b2fare[i]
			} else {
				i++
			}
		}
	case 5: //按Departure,Arrival,Airline,FareBase删除,FareV2票单自动插入时删除的
		Airline := colText[:2]
		as := strings.Split(colText[2:], " ")
		FareBase := as[0]
		CommandID, _ := strconv.Atoi(as[1])
		for i := 0; i < DSLen; {
			if this.b2fare[i].BillID == "FareV2" &&
				this.b2fare[i].AirInc == Airline &&
				this.b2fare[i].FareBase == FareBase &&
				this.b2fare[i].CommandID != CommandID {
				DSLen--
				this.b2fare[i], this.b2fare[DSLen] = this.b2fare[DSLen], this.b2fare[i]
			} else {
				i++
			}
		}
	}

	if DSLen != len(this.b2fare) {
		this.b2fare = this.b2fare[:DSLen]
	}

	this.mutex2.Unlock()

	return nil
}


//插入票单
func (this *DatesFares) InsertFB(fare *mysqlop.MainBill) {
	this.mutex2.Lock()
	this.b2fare = append(this.b2fare, fare)
	this.mutex2.Unlock()
}

//插入GDS
func (this *DatesFares) InsertGDS(fare *mysqlop.MainBill) {
	fq := &FareQueue{fare, nil}
	var header *FareQueue
	ok := true

	this.mutexgds.Lock()
	if header, ok = this.queue[fare.TravelFirstDate]; !ok {
		this.queue[fare.TravelFirstDate] = fq
	} else {
		for ; header.next != nil; header = header.next {
		}
		header.next = fq
	}
	this.mutexgds.Unlock()

	if !ok {
		go this.DealQueue(fare.TravelFirstDate)
	}
}

var queuewait = time.Second / 20

func (this *DatesFares) DealQueue(traveldate string) {
	defer errorlog.DealRecoverLog()

	this.mutexgds.RLock()
	bytesdate := this.gdsfare[traveldate]
	header, ok2 := this.queue[traveldate]
	this.mutexgds.RUnlock()

	var faresdata mysqlop.ListMainBill
	json.Unmarshal(bytesdate, &faresdata)

	if !ok2 || header == nil {
		this.mutexgds.Lock()
		delete(this.queue, traveldate)
		this.mutexgds.Unlock()
		return
	}

	faresLen := len(faresdata)
	done := 0

	for header != nil && header.fare != nil {
		fare := header.fare

		if fare.Routine == "Overdue" {
			j := faresLen
			for i := 0; i < j; {
				if faresdata[i].Agency == fare.Agency &&
					faresdata[i].PCC == fare.PCC &&
					faresdata[i].Trip == fare.Trip &&
					faresdata[i].GoorBack == fare.GoorBack &&
					faresdata[i].TravelLastDate == fare.TravelLastDate {
					j--
					faresdata[i], faresdata[j] = faresdata[j], faresdata[i]
				} else {
					i++
				}
			}

			if j != faresLen {
				faresdata = faresdata[:j]
				faresLen = j
			}

		} else {
			faresdata = append(faresdata, fare)
			faresLen++
		}

		done++
		if header.next != nil && done >= 100 {
			if bytesdate, err := json.Marshal(faresdata); err == nil {
				this.mutexgds.Lock()
				this.gdsfare[traveldate] = bytesdate
				this.mutexgds.Unlock()
			}
			done = 0
		}

		if header.next == nil { //这里是为了防止CPU频繁切换导致峰值很高,所以等待100ms, 看后面是否有队列数据进入
			done = done + 5
			time.Sleep(queuewait)
		}

		this.mutexgds.Lock() //防止插入和获取同时并发
		header = header.next
		if header == nil {
			delete(this.queue, traveldate)
			if faresLen > 0 {
				if bytesdate, err := json.Marshal(faresdata); err == nil {
					this.gdsfare[traveldate] = bytesdate
				} else {
					delete(this.gdsfare, traveldate)
				}
			} else {
				delete(this.gdsfare, traveldate)
			}
		}
		this.mutexgds.Unlock()
	}
}


func init() {


	QueryDepart.DepartStation = make(map[string]*OtherStation, 5000)
	QueryArrive.DepartStation = make(map[string]*OtherStation, 5000)

	CommandCache.Command = make(map[int]*MessQueue, 10000)
	RuleCondition.Condition = make(map[string]*RuleRecord, 10000)

	Cabin = make(map[string]string, 5000)
	RuleTotalParse = new(RuleItemParse)
	commMission = new(CommandMission)
	commMission.exec = make([]*CommMissionExec, 0, 10)


}



/***查询是否存在飞行航帮记录&如果没有返回中转地***/
func RoutineCheckSelect(
	DepartStation, //出发地
	ArriveStation, //目的地
	TravelDate string) ( //旅行日期
	connStation []string, //无航线时(havefare==false)可转的中转地
	havefare bool, //是否有缓存有fare记录,havefare==true标识有直接可用fare
	err error) {


		//加锁
	QueryDepart.mutex.RLock()
	//查询，是否有这个出发地（如果压根就没有这个出现地，则直接返回，没必要往下看了）
	departOther, ok := QueryDepart.DepartStation[DepartStation]
	QueryDepart.mutex.RUnlock()
	if !ok {
		return nil, false, errors.New("No DepartStation Cache")
	}

	departOther.mutex.RLock()
	//查询是否有对应的目的地
	datesfares, ok := departOther.ArriveStation[ArriveStation]
	departOther.mutex.RUnlock()

	if ok {
		for _, fare := range datesfares.b2fare {
			if fare.TravelFirstDate <= TravelDate &&
				TravelDate <= fare.TravelLastDate { //不再检测预订时间,其实没真实意义了
				return nil, true, nil
			}
		}
	}

	//找不到飞行航线,必须匹配中转目的地了
	QueryArrive.mutex.RLock()
	//这里是从目的地开始查了
	arriveOther, ok := QueryArrive.DepartStation[ArriveStation]
	QueryArrive.mutex.RUnlock()

	if !ok {
		return nil, false, errors.New("No Arrivetation Cache")
	}

	//利用departOther里面的ArriveStation的长度来当容量
	mapStation := make(map[string]bool, len(departOther.ArriveStation))
	departOther.mutex.RLock()
	for station := range departOther.ArriveStation {
		mapStation[station] = true
	}
	departOther.mutex.RUnlock()

	connStation = make([]string, 0, 5)
	arriveOther.mutex.RLock()
	for station := range arriveOther.ArriveStation {
		if mapStation[station] {
			connStation = append(connStation, station)
		}
	}
	arriveOther.mutex.RUnlock()

	return connStation, false, nil
}

/************fare查询及接口****************/

//最后返回一个ListFare和error出去
func QueryFareCommon(
	QS *QueryStation,
	DepartStation string,  //出发
	ArriveStations []string, //目地
	TravelDate string,//旅行日期
	Wkday int,
	DayDiff int,//距离今天多少天
	Trip int,  //单程或者往返
	GoorBack int, //
	Stay int,  //呆
	Airline []string, //航司
	BookingClass string, //舱位
	TheTrip bool,
	BackDate string) ( //Special to GDS;去程的回程日期,回程的去程日期
	ListFare []*mysqlop.MainBill,
	err error) {

		//先进行加锁，接着来查询QS.DepartStation中的【DepartStation】;如果查询得到 departother
		//在那里缓存的？？？需要查清楚在哪里缓存。不然不知道数据是怎么进去的。要怎么拿出来》
	QS.mutex.RLock()
	departother, ok := QS.DepartStation[DepartStation]
	QS.mutex.RUnlock()

	if !ok {
		return nil, errors.New("No DepartStation Cache")
	}

	AirInc := strings.Join(Airline, " ")
	BillBerth := BookingClass
	if BookingClass == "Y" {
		BillBerth += "P"
	}

	ListFare = make([]*mysqlop.MainBill, 0, 150)

	for _, ArriveStation := range ArriveStations {
		departother.mutex.RLock()
		datesfares, ok := departother.ArriveStation[ArriveStation]
		departother.mutex.RUnlock()

		if !ok || len(datesfares.b2fare) == 0 {
			continue
		}

		for _, fare := range datesfares.b2fare {
			if fare.TravelFirstDate <= TravelDate && TravelDate <= fare.TravelLastDate &&
				fare.WeekFirst <= Wkday && Wkday <= fare.WeekLast &&
				fare.ApplyHumen == "A" &&
				(fare.OutBill2 == 365 || fare.OutBill2 <= DayDiff) &&
				(!TheTrip && fare.Trip == 0 || fare.Trip == Trip /*1*/ && fare.GoorBack == GoorBack &&
					fare.MinStay <= Stay && Stay <= fare.MaxStay) &&
				(AirInc == "" || strings.Contains(AirInc, fare.AirInc)) &&
				strings.Contains(BillBerth, fare.BillBerth) {
				ListFare = append(ListFare, fare)
			}
		}
	}

	//GDS Fare
	for _, ArriveStation := range ArriveStations {
		departother.mutex.RLock()
		datesfares, ok := departother.ArriveStation[ArriveStation]
		departother.mutex.RUnlock()
		if !ok {
			continue
		}

		datesfares.mutexgds.RLock()
		bytesdata, ok := datesfares.gdsfare[TravelDate]
		datesfares.mutexgds.RUnlock()
		if !ok {
			continue
		}

		var fares mysqlop.ListMainBill
		json.Unmarshal(bytesdata, &fares)

		for _, fare := range fares {
			if (!TheTrip && fare.Trip == 0 ||
				fare.Trip == Trip && fare.GoorBack == GoorBack) &&
				fare.TravelLastDate == BackDate &&
				(AirInc == "" || strings.Contains(AirInc, fare.AirInc)) &&
				strings.Contains(BillBerth, fare.BillBerth) {
				ListFare = append(ListFare, fare)
			}
		}
	}

	if len(ListFare) > 0 {
		return ListFare, nil
	} else {
		return nil, errors.New("No ArriveStation Cache")
	}

}


func QueryFareDays(

	QS *QueryStation,
	DepartStation string,
	ArriveStations []string,
	TravelDate string,
	Wkday int, DayDiff int, Trip int, GoorBack int, Stay int,
	Airline []string, BookingClass string, Days int, TheTrip bool,
	BackDate string) (
	ListFare mysqlop.MutilDaysMainBill,
	err error) {


	date, _ := time.Parse("2006-01-02", TravelDate)
	ListFare.Fares = make([][2]mysqlop.ListMainBill, Days)
	Daycount := 0

NextDay:
	ListFare.Fares[Daycount][0] = mysqlop.ListMainBill{}
	if fares, err := QueryFareCommon(QS, DepartStation, ArriveStations, TravelDate,
		Wkday, DayDiff, Trip, GoorBack, Stay, Airline, BookingClass, TheTrip, BackDate); err == nil {
		ListFare.Fares[Daycount][0] = append(ListFare.Fares[Daycount][0], fares...)
	}

	Days--
	if Days > 0 {
		date = date.AddDate(0, 0, 1)
		Daycount++
		TravelDate = date.Format("2006-01-02")
		Wkday++
		if Wkday == 8 {
			Wkday = 1
		}
		goto NextDay
	}

	return
}

func QueryFareLegsDays(
	QS *QueryStation,
	DepartStation string,
	ArriveStations []string,
	TravelDate string,
	Wkday int, DayDiff int, Trip int, GoorBack int, Stay int,
	Airline []string, BookingClass string, Legs int, Days int, TheTrip bool,
	BackDate string) (
	ListFare mysqlop.MutilDaysMainBill,
	err error) {

	date, _ := time.Parse("2006-01-02", TravelDate)
	ListFare.Fares = make([][2]mysqlop.ListMainBill, Days)
	Daycount := 0

NextDay:
	ListFare.Fares[Daycount][0] = mysqlop.ListMainBill{}
	if fares, err := QueryFareCommon(QS, DepartStation, ArriveStations, TravelDate,
		Wkday, DayDiff, Trip, GoorBack, Stay, Airline, BookingClass, TheTrip, BackDate); err == nil {
		for _, fare := range fares {
			if (len(fare.Routine)-3)/7 == Legs {
				ListFare.Fares[Daycount][0] = append(ListFare.Fares[Daycount][0], fare)
			}
		}
	}

	Days--
	if Days > 0 {
		date = date.AddDate(0, 0, 1)
		Daycount++
		TravelDate = date.Format("2006-01-02")
		Wkday++
		if Wkday == 8 {
			Wkday = 1
		}
		goto NextDay
	}

	return
}



/************fare查询应用***********************/

//传入Point2Point_In查询条件，BackDate回程
func QueryFare(p2p_in *webapi.Point2Point_In, BackDate string,
	cListFare chan mysqlop.MutilDaysMainBill) {

	var (
		QS             *QueryStation
		DepartStations []string
		ArriveStations []string
		ListFare       mysqlop.MutilDaysMainBill
	)

	//最后就是将这个获取到的ListFare 赋值给cListFare。接着cListFare再次赋值给上一个页面
	defer func() {
		cListFare <- ListFare
	}()

	/*if p2p_in.Deal == "QueryDepartDays" ||
		p2p_in.Deal == "QueryDepartLegsDays" {
		QS = &QueryDepart
	} else if p2p_in.Deal == "QueryArriveDays" ||
		p2p_in.Deal == "QueryArriveLegsDays" {QS = &QueryArrive
	} else {
		errorlog.WriteErrorLog("QueryFare (1): ")
		return
	}*/
	QS = &QueryDepart

	ListFare.Fares = make([][2]mysqlop.ListMainBill, p2p_in.Days)

	for rc, leg := range p2p_in.Rout {

		if rc == 0 && len(p2p_in.Rout) == 2 {
			BackDate = p2p_in.Rout[1].TravelDate
		} else if rc == 1 && len(p2p_in.Rout) == 2 {
			BackDate = p2p_in.Rout[0].TravelDate
		} else if rc > 1 {
			break
		}

		//if p2p_in.Deal == "QueryDepartDays" ||
		//	p2p_in.Deal == "QueryDepartLegsDays" {
		DepartStations = leg.DepartCounty
		ArriveStations = leg.ArriveCounty
		//} else {
		//	DepartStations = leg.ArriveCounty
		//	ArriveStations = leg.DepartCounty
		//}


		for _, departstation := range DepartStations {
			var fares mysqlop.MutilDaysMainBill
			var err error
			if p2p_in.Deal == "QueryDepartDays" ||
				p2p_in.Deal == "QueryArriveDays" {

					//如果是查询出发天或者查询到达天的话
				fares, err = QueryFareDays(QS, departstation, ArriveStations, leg.TravelDate,
					leg.Wkday, leg.DayDiff, leg.Trip, leg.GoorBack, leg.Stay,
					p2p_in.Airline, p2p_in.BerthType, p2p_in.Days, p2p_in.TheTrip, BackDate)

			} else {

				fares, err = QueryFareLegsDays(QS, departstation, ArriveStations, leg.TravelDate,
					leg.Wkday, leg.DayDiff, leg.Trip, leg.GoorBack, leg.Stay,
					p2p_in.Airline, p2p_in.BerthType, p2p_in.Legs, p2p_in.Days, p2p_in.TheTrip, BackDate)
			}

			if err == nil {

				for day := 0; day < p2p_in.Days; day++ {
					ListFare.Fares[day][rc] = append(ListFare.Fares[day][rc], fares.Fares[day][0]...)
				}
			}
		}
	}
}

//基本功能与QueryFare相同,只是MutilQueryFare是并发获取的
func MutilQueryFare(p2p_in *webapi.Point2Point_In,
	cListFare chan mysqlop.MutilDaysMainBill) {

	var (
		ListFare mysqlop.MutilDaysMainBill
		cFares   []chan mysqlop.MutilDaysMainBill
		BackDate string
	)

	cFares = make([]chan mysqlop.MutilDaysMainBill, len(p2p_in.Rout))

	for i, leg := range p2p_in.Rout {
		p2p_tmp := *p2p_in
		p2p_tmp.Rout = []*mysqlop.Routine{leg}
		if len(leg.DepartCounty) > len(leg.ArriveCounty) {
			p2p_tmp.Deal = "QueryArriveDays"
		}

		if i == 0 && len(p2p_in.Rout) == 2 {
			BackDate = p2p_in.Rout[1].TravelDate
		} else if i == 1 && len(p2p_in.Rout) == 2 {
			BackDate = p2p_in.Rout[0].TravelDate
		}

		cFares[i] = make(chan mysqlop.MutilDaysMainBill, 1)
		go QueryFare(&p2p_tmp, BackDate, cFares[i])
	}

	for i := range p2p_in.Rout {
		if i == 0 {
			ListFare = <-cFares[0]
		} else {
			fares := <-cFares[i]
			for day := range ListFare.Fares {
				ListFare.Fares[day][i] = fares.Fares[day][0]
			}
		}
	}

	cListFare <- ListFare
}

/***命令缓存(这个缓存主要时为了缓存所有使用过的命令)****/
var CommandCache struct {
	Command map[int]*MessQueue
	mutex   sync.RWMutex
}

var CommandID errorlog.AddID //命令行编号,递增(启动时获取记录最大值)


func getCID() int {
	return CommandID.GetID()
}

var doingCommand = struct {
	queue map[int]*MessQueue
	mutex sync.RWMutex
}{
	queue: make(map[int]*MessQueue, 10),
}
var MaxCommandDeamon = 3

//命令任务队列(这里时未完成的命令)
var commMission *CommandMission

/**********翻译后条款内容缓存*********************/
var RuleCondition struct {
	Condition map[string]*RuleRecord //string=DepartStation + ArriveStation + Airline + FareBase
	mutex     sync.RWMutex
}

/**********翻译格式缓存*********************/
var RuleTotalParse *RuleItemParse
var RuleID errorlog.AddID  // 这个值在获取最大的RuleTranslate上面有涉及到

func getRID() int {
	return RuleID.GetID()
}

/**********Fare缓存时的临时ID******************/
var FareID errorlog.AddID //标识Fare序列号MainBill.ID

func getID() int {
	return FareID.GetID()
}

