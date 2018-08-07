package webapi

import (
	"bytes"
	"cacheflight"
	"cachestation"
	"encoding/json"
	"errorlog"
	"fmt"
	"io/ioutil"
	"mysqlop"
	"net/http"
	"outsideapi"
	"sort"
	"strconv"
	"strings"
	"time"
)


type sPipe struct {
	Agency string  //供应商
	Day    int  //
	Index  int //组合供应商4个Shopping的排序
	Info   interface{}
}


type sFares struct {
	Fs  [][]mysqlop.ListMainBill //[Days][Go or Back...]
	Ris []*MB2RouteInfo          //静态票单数据归类缩小
	p   *sFaresFlights
}

type sFlights struct {
	Fs    [][]cacheflight.FlightJSON //[Days][Go/Back][...]<==[Go/Back][Days]
	FsDyn [][]cacheflight.FlightJSON //动态查询的航班数据[Days][Go/Back]   //第N天/去程或者回程 航班数据
	p     *sFaresFlights
}

type sFaresFlights struct {
	Fares            *sFares
	Flights          *sFlights
	fareOutChan      chan mysqlop.ListMainBill   //Used By MixAgent
	fareInChan       chan mysqlop.ListMainBill   //Used By MixAgent
	flightOutChan    chan cacheflight.FlightJSON //Used By MixAgent
	flightInChan     chan cacheflight.FlightJSON //Used By MixAgent
	flightDynOutChan chan cacheflight.FlightJSON //Used By MixAgent
	flightDynInChan  chan cacheflight.FlightJSON //Used By MixAgent
	p                *sProvider
}


//提供单步单天shopping
type sProvider struct {
	//Out
	SourceAgency string                 //ShowShoppingName
	Agent        *cachestation.Agencies //处理的供应商
	P2pi         *Point2Point_In        //查询条件
	FsFs         *sFaresFlights         //很多个供应商引用同一数据
	Day          int                    //此供应商处理是第几个fsfs索引
	addDays      int                    //双票(Cache)获取信息必须增加的日期数(shopping多加的天数)
	addOutDay    int                    //双票去程第2票某程增加天数
	addInDay     int                    //双票回程第2票某程增加天数
	DealID       int                    //分批处理的处理号
	dealOut      chan *sPipe            //处理完传出到sShopping

	//In
	rout            []*mysqlop.Routine  //合并queryRout中的数据的票单航线
	queryRout       [][2][]string       //保存从getQueryRoutine获取的机场路线(组合供应商)
	useRoutine      map[string]struct{} //使用的专有路线(组合供应商时要求加入组合的航线)
	theTrip         bool                //(双票)加入前续票单的要求(要求主票单Trip=1,主票单不可2个单程组合,票单获取时使用)
	thePrefixSuffix bool                //(双票)前续票单Airline相同,当theTrip=true时,thePrefixSuffix相当于无效.
	cycle           int                 //成功处理的KsKs记录中的Flight序号(Segment编号)
	mixModel        bool                //(true时)Cache中接受信息.(混合使用其它shopping各单程)
	agentLen        int                 //SourceAgency的组合供应商的长度
	synchronous     chan struct{}       //当Cache有多天时,同步数据(从sShopping同步)
}


//提供某一供应商多天shopping(分多次按天输出)
type sShopping struct {
	//ps[0]==OUT(one)-->IN(one)
	//ps[1]==OUT(one)-->IN(one+1)
	//ps[2]==OUT(two)-->IN(two)
	//ps[3]==OUT(two+1)-->IN(two)
	//ps      [][4]*sProvider          //[day][4]
	SourceAgency  string                    //供应商组合的ShowShoppingName(非组合就是第1个供应商//ShowShoppingName)
	Agents        []*cachestation.Agencies  //模型,(1)1个供应商,(2)2个供应商组合
	P2pi          *Point2Point_In           //查询条件
	ticketCount   int                       //组合供应商制作的票数(2 or 4)
	sourceRoutine []*cachestation.AgRoutine //给供应商原始路线表
	dealIn        chan *sPipe               //接受sProvider的传入
	dealOut       chan *sPipe               //输出到sProduction
}

//提供组合产品
type sProdution struct {

	P2pi       *Point2Point_In
	totalCount int
	dealIn     chan *sPipe                 //接受sShopping的传入
	dealOut    chan ListPoint2Point_Output //输出到sOutAgent
}

//归类票单数据
func (this *sFares) Calssify(second bool) {
	if second {
		if this.p.p.Agent.SaveAs != "KeyCache" {
			this.Ris, _ = InterJourney(this.Ris, len(this.p.p.rout), second, this.p.p.thePrefixSuffix)
		}
	} else {
		back := MainBill2RouteInfo_V2(this.p.p.Agent, this.Fs[this.p.p.Day], this.p.p.rout)

		if this.p.p.Agent.SaveAs == "Cache" { //KeyCache双程路线可能不同
			_, back = InterJourney(back, len(this.p.p.rout), second, this.p.p.thePrefixSuffix)
		}

		if len(this.p.p.useRoutine) == 0 || len(back) == 0 {
			this.Ris = back
			return
		}

		tmp := make([]*MB2RouteInfo, 0, len(back))
		var ok1, ok2 bool
		for _, ri := range back {
			ok1, ok2 = false, false
			_, ok1 = this.p.p.useRoutine[ri.Routine]
			_, ok2 = this.p.p.useRoutine[RedoRoutineMutil(ri.Routine)]
			if ok1 || ok2 {
				tmp = append(tmp, ri)
			}
		}
		this.Ris = tmp
	}
}

func (this *sFares) MarkNoget() {
	markNoget(this.Ris)
}

func (this *sFares) MergeMiddleMatch() {
	MergeMiddleMatch(this.Ris, len(this.p.p.rout))
}

func (this *sFlights) Matching() {
	escape := make(map[string]struct{}, len(this.Fs[0])*len(this.Fs)+5)
	for _, mb := range this.p.Fares.Ris {
		for _, ds := range this.p.p.Agent.DataSource {
			escape[mb.Routine+"/"+ds+mb.PCC] = struct{}{}
		}
	}

	for seg, f := range this.Fs[this.p.p.Day] {
		newfore := make([]*cacheflight.RoutineInfoStruct, 0, len(f.Route))
		for _, rout := range f.Route {
			_, ok := escape[rout.R+"/"+rout.FI[0].Legs[0].P+rout.FI[0].Legs[0].PCC]
			if ok {
				cilen := 0
				for cilen := 0; cilen < len(rout.FI[0].Legs[0].CI); cilen++ {
					if rout.FI[0].Legs[0].CI[cilen].WI == 2 {
						break
					}
				}
				if cilen != len(rout.FI[0].Legs[0].CI) {
					newfore = append(newfore, rout)
				}
			}
		}

		this.Fs[this.p.p.Day][seg].Route = newfore
		sort.Sort(this.Fs[this.p.p.Day][seg].Route)
	}
}

func (this *sFaresFlights) Cache() {
	if this.p.Day != 0 {
		<-this.p.synchronous
		return
	}

	if this.p.synchronous != nil {
		defer func() {
			for i := 1; i < this.p.P2pi.Days+this.p.addDays; i++ {
				this.p.synchronous <- struct{}{}
			}
		}()
	}

	mdmbChan := make(chan *mysqlop.MutilDaysMainBill, len(this.p.queryRout))
	fssChan := make(chan [][]*cacheflight.FlightJSON, len(this.p.queryRout))
	Len := len(this.p.rout)
	traveldate, backdate := computeDate(this.p.rout, this.p.Day, this.p.addOutDay, this.p.addInDay)

	for i := 0; i < len(this.p.queryRout); i++ {
		rout := make([]*mysqlop.Routine, Len)
		if Len == 1 {
			r0 := *this.p.rout[0]
			r0.DepartCounty = this.p.queryRout[i][0]
			r0.ArriveCounty = this.p.queryRout[i][1]
			r0.TravelDate = traveldate
			rout[0] = &r0
		} else {
			r0 := *this.p.rout[0]
			r0.DepartCounty = this.p.queryRout[i][0]
			r0.ArriveCounty = this.p.queryRout[i][1]
			r0.TravelDate = traveldate
			rout[0] = &r0
			r1 := *this.p.rout[1]
			r1.DepartCounty = this.p.queryRout[i][1]
			r1.ArriveCounty = this.p.queryRout[i][0]
			r1.TravelDate = backdate
			rout[1] = &r1
		}

		go QueryFareAndFlighttime_V2(this.p.addDays, this.p.P2pi, rout, mdmbChan, fssChan)
	}

	for index := 0; index < len(this.p.queryRout)*2; index++ {
		select {
		case mdmb := <-mdmbChan:
			for i := range mdmb.Fares {
				if this.Fares.Fs[i] == nil {
					this.Fares.Fs[i] = make([]mysqlop.ListMainBill, Len)
				}
				//fmt.Println("mdmb", i, len(this.Fares.Fs[i][0]), len(mdmb.Fares[i][0]), len(this.Fares.Fs[i][1]), len(mdmb.Fares[i][1]))
				if this.Fares.Fs[i][0] == nil {
					this.Fares.Fs[i][0] = mdmb.Fares[i][0]
				} else {
					this.Fares.Fs[i][0] = append(this.Fares.Fs[i][0], mdmb.Fares[i][0]...)
				}

				if Len > 1 {
					if this.Fares.Fs[i][1] == nil {
						this.Fares.Fs[i][1] = mdmb.Fares[i][1]
					} else {
						this.Fares.Fs[i][1] = append(this.Fares.Fs[i][1], mdmb.Fares[i][1]...)
					}
				}
			}
		case fss := <-fssChan:
			for i := range fss[0] {
				if this.Flights.Fs[i] == nil {
					this.Flights.Fs[i] = make([]cacheflight.FlightJSON, Len)
				}
				//fmt.Println("fss", i, len(this.Flights.Fs[i][0].Route), len(fss[0][i].Route), len(this.Flights.Fs[i][1].Route), len(fss[1][i].Route))
				if this.Flights.Fs[i][0].Route == nil {
					this.Flights.Fs[i][0] = *fss[0][i]
				} else {
					this.Flights.Fs[i][0].Route = append(this.Flights.Fs[i][0].Route, fss[0][i].Route...)
				}

				if Len > 1 {
					if this.Flights.Fs[i][1].Route == nil {
						this.Flights.Fs[i][1] = *fss[1][i]
					} else {
						this.Flights.Fs[i][1].Route = append(this.Flights.Fs[i][1].Route, fss[1][i].Route...)
					}
				}
			}
		}
	}
}

func (this *sFaresFlights) KeyCache() {

	traveldate, backdate := computeDate(this.p.rout, this.p.Day, this.p.addOutDay, this.p.addInDay)
	this.Fares.Fs[this.p.Day] = make([]mysqlop.ListMainBill, len(this.p.rout))
	this.Flights.Fs[this.p.Day] = make([]cacheflight.FlightJSON, len(this.p.rout))

	bcc := make(chan *cacheflight.BlockCache, len(this.p.queryRout))
	for i := 0; i < len(this.p.queryRout); i++ {
		go getBlockCache_V2(this.p.Agent.Agency, this.p.queryRout[i][0], this.p.queryRout[i][1], traveldate, backdate, this.p.P2pi.Quick, bcc)
	}

	for i := 0; i < len(this.p.queryRout); i++ {
		bc := <-bcc
		if bc == nil || bc.Flights == nil || bc.Fares.Mainbill == nil {
			continue
		}

		Len := len(bc.Fares.Mainbill)
		tmpi := this.p.DealID*1000000000 + i*10000 //建立一个与FB.MainBill.ID不同的范围ID
		for ti, mb := range bc.Fares.Mainbill {
			mb.ID = tmpi + ti
		}

		if backdate != "" {
			Len = Len / 2
			if this.Fares.Fs[this.p.Day][1] == nil {
				this.Fares.Fs[this.p.Day][1] = bc.Fares.Mainbill[Len:]
				this.Flights.Fs[this.p.Day][1] = bc.Flights[1]
			} else {
				this.Fares.Fs[this.p.Day][1] = append(this.Fares.Fs[this.p.Day][1], bc.Fares.Mainbill[Len:]...)
				this.Flights.Fs[this.p.Day][1].Route = append(this.Flights.Fs[this.p.Day][1].Route, bc.Flights[1].Route...)
			}
		}

		if this.Fares.Fs[this.p.Day][0] == nil {
			this.Fares.Fs[this.p.Day][0] = bc.Fares.Mainbill[:Len]
			this.Flights.Fs[this.p.Day][0] = bc.Flights[0]
		} else {
			this.Fares.Fs[this.p.Day][0] = append(this.Fares.Fs[this.p.Day][0], bc.Fares.Mainbill[:Len]...)
			this.Flights.Fs[this.p.Day][0].Route = append(this.Flights.Fs[this.p.Day][0].Route, bc.Flights[0].Route...)
		}
	}
}

func (this *sFaresFlights) MatchingLegs() {
	for i := range this.p.rout { //i+1 == rout[i].Segment
		if this.Flights.FsDyn[this.p.Day] != nil {
			this.p.cycle = MainBillMatchingLegs(this.p.Agent, this.p.DealID, this.p.cycle, this.Flights.FsDyn[this.p.Day],
				this.Fares.Ris, this.p.rout[i].Segment, this.p.P2pi.DeviationRate, this.p.P2pi.ConnMinutes, this.p.P2pi.ShowBCC)
		} else {
			this.p.cycle = MainBillMatchingLegs(this.p.Agent, this.p.DealID, this.p.cycle, this.Flights.Fs[this.p.Day],
				this.Fares.Ris, this.p.rout[i].Segment, this.p.P2pi.DeviationRate, this.p.P2pi.ConnMinutes, this.p.P2pi.ShowBCC)
		}
	}
}

func (this *sFaresFlights) GetDynFlight() {
	if this.p.mixModel {
		this.Flights.FsDyn[this.p.Day] = make([]cacheflight.FlightJSON, len(this.p.rout))
		if len(this.p.rout) > 1 {
			add := 0
			for add < 2 {
				select {
				case flights := <-this.flightDynOutChan:
					this.Flights.FsDyn[this.p.Day][0].Route = append(this.Flights.FsDyn[this.p.Day][0].Route, flights.Route...)
					add++
				case flights := <-this.flightDynInChan:
					this.Flights.FsDyn[this.p.Day][1].Route = append(this.Flights.FsDyn[this.p.Day][1].Route, flights.Route...)
					add++
				case <-time.After(waitSecond - time.Second/2):
					return
				}
			}
		} else {
			select {
			case flights := <-this.flightDynOutChan:
				this.Flights.FsDyn[this.p.Day][0].Route = append(this.Flights.FsDyn[this.p.Day][0].Route, flights.Route...)
				return
			case <-time.After(waitSecond - time.Second/2):
				return
			}
		}
		return
	}

	defer func() {
		if this.flightDynOutChan != nil {
			this.flightDynOutChan <- this.Flights.FsDyn[this.p.Day][0]
		}

		if this.flightDynInChan != nil {
			this.flightDynInChan <- this.Flights.FsDyn[this.p.Day][1]
		}
	}()

	type getFlight struct {
		Segment       int
		TravelDate    string
		ListAirline1E []string
		ListAirline1B []string
		c_fjson       chan *cacheflight.FlightJSON
	}

	this.Flights.FsDyn[this.p.Day] = make([]cacheflight.FlightJSON, len(this.p.rout))
	getData := make(map[string]*getFlight, 10) //string=DepartStation+ArriveStation
	hadget := make(map[string]struct{}, 20)    //已经远程获取数据的航线
	mid := make(map[int]struct{}, len(this.Fares.Fs[this.p.Day]))
	traveldate, backdate := computeDate(this.p.rout, this.p.Day, this.p.addOutDay, this.p.addInDay)

	//把已经成功获取的MB.ID记录起来,这里同时必须记录Routine生成的所在'$'分隔才是更合理的.
	//这里解决的是多Routine问题
	for _, ri := range this.Fares.Ris {
		for _, mm := range ri.MM {
			for _, id := range mm.doneID {
				mid[id] = struct{}{}
			}
		}

		for _, mm := range ri.MMBCC {
			for _, id := range mm.doneID {
				mid[id] = struct{}{}
			}
		}
	}

	for _, ri := range this.Fares.Ris {
		if len(ri.MM) > 0 || len(ri.MMBCC) > 0 || ri.noget {
			continue
		}

		//这里处理,如果一条Fare记录已经存在Shopping成功的Routine,那么不再到远程获取没成功的Routine.
		comeback := true
		for _, mb := range ri.ListMB {
			if _, ok := mid[mb.ID]; !ok {
				comeback = false //还可以再提高：如果同航线低舱位有了,不同再获取高舱位航班
				break
			}
		}
		if comeback {
			continue
		}

		DepartStation := ri.Routine[:3] //动态票单的Rotine是多样化的.
		ArriveStation := ri.Routine[len(ri.Routine)-3:]
		Airline := ri.ListMB[0].AirInc //这里绝大多数是正确的,如果航线中存在不同航司,也会是同集团的.

		if _, ok := hadget[DepartStation+Airline+ArriveStation]; ok { //这种组合只是因为远程会自动返回多种中转
			continue //还可以再提高：如果同航线低舱位有了,不同再获取高舱位航班
		} else {
			hadget[DepartStation+Airline+ArriveStation] = struct{}{}
		}

		if getflight, ok := getData[DepartStation+ArriveStation]; ok {
			if Airline1E[Airline] {
				getflight.ListAirline1E = append(getflight.ListAirline1E, Airline)
			} else {
				getflight.ListAirline1B = append(getflight.ListAirline1B, Airline)
			}
		} else {
			td := traveldate
			if ri.Segment == 2 {
				td = backdate
			}
			getflight = &getFlight{
				Segment:       ri.Segment,
				TravelDate:    td,
				ListAirline1E: []string{},
				ListAirline1B: []string{},
				c_fjson:       make(chan *cacheflight.FlightJSON, 2)}

			if Airline1E[Airline] {
				getflight.ListAirline1E = append(getflight.ListAirline1E, Airline)
			} else {
				getflight.ListAirline1B = append(getflight.ListAirline1B, Airline)
			}

			getData[DepartStation+ArriveStation] = getflight
		}
	}

	if len(getData) == 0 {
		goto No_getData //直接跳过,这样减少步骤,会快很多
	}

	for DepartArrive, Get := range getData {
		if len(Get.ListAirline1E) > 0 {
			go outsideapi.SearchFlightJSON_TO_V2(DepartArrive[:3], DepartArrive[3:], Get.TravelDate, strings.Join(Get.ListAirline1E, " "), "1E", Get.c_fjson)
		} else {
			Get.c_fjson <- nullFlightJSON
		}

		if len(Get.ListAirline1B) > 0 {
			go outsideapi.SearchFlightJSON_TO_V2(DepartArrive[:3], DepartArrive[3:], Get.TravelDate, strings.Join(Get.ListAirline1B, " "), "1B", Get.c_fjson)
		} else {
			Get.c_fjson <- nullFlightJSON
		}
	}

	for i := range this.Flights.FsDyn[this.p.Day] {
		this.Flights.FsDyn[this.p.Day][i].Route = make([]*cacheflight.RoutineInfoStruct, 0, 100)
	}

	for _, Get := range getData {
		for i := 0; i < 2; i++ {
			select {
			case <-time.After(waitSecond - time.Second):
				goto No_getData
			case outout := <-Get.c_fjson:
				if outout.Route != nil {
					if Get.Segment == 1 {
						this.Flights.FsDyn[this.p.Day][0].Route = append(this.Flights.FsDyn[this.p.Day][0].Route, outout.Route...)
					} else {
						this.Flights.FsDyn[this.p.Day][1].Route = append(this.Flights.FsDyn[this.p.Day][1].Route, outout.Route...)
					}
				}
			}
		}
	}

No_getData:
	if this.p.P2pi.Debug {
		if len(getData) > 0 {
			//writeLog(dc, DealID, rmb, qfi_fore, qfi_fore_web)
		} else {
			//writeLog(dc, DealID, rmb, qfi_fore, nil)
		}
	}
}


//#TODO 在这里调用了---》引发keycache的
func (this *sProvider) ReadFsFs() {
	if this.mixModel {
		this.FsFs.Fares.Fs[this.Day] = make([]mysqlop.ListMainBill, len(this.rout))
		this.FsFs.Flights.Fs[this.Day] = make([]cacheflight.FlightJSON, len(this.rout))
		add := 0
		if len(this.rout) > 1 {
			for add < 4 {
				select {
				case fares := <-this.FsFs.fareOutChan: //使用append插入,主要是为了防止fare的地址被多个shopping共用.
					this.FsFs.Fares.Fs[this.Day][0] = append(this.FsFs.Fares.Fs[this.Day][0], fares...)
					add++ //以上句子是为了防止他们功用一个切片空间,导致地址覆盖
				case flights := <-this.FsFs.flightOutChan:
					this.FsFs.Flights.Fs[this.Day][0].Route = append(this.FsFs.Flights.Fs[this.Day][0].Route, flights.Route...)
					add++
				case fares := <-this.FsFs.fareInChan:
					this.FsFs.Fares.Fs[this.Day][1] = append(this.FsFs.Fares.Fs[this.Day][1], fares...)
					add++
				case flights := <-this.FsFs.flightInChan:
					this.FsFs.Flights.Fs[this.Day][1].Route = append(this.FsFs.Flights.Fs[this.Day][1].Route, flights.Route...)
					add++
				case <-time.After(waitSecond):
					return
				}
			}
		} else {
			for add < 2 {
				select {
				case fares := <-this.FsFs.fareOutChan:
					this.FsFs.Fares.Fs[this.Day][0] = append(this.FsFs.Fares.Fs[this.Day][0], fares...)
					add++
				case flights := <-this.FsFs.flightOutChan:
					this.FsFs.Flights.Fs[this.Day][0].Route = append(this.FsFs.Flights.Fs[this.Day][0].Route, flights.Route...)
					add++
				case <-time.After(waitSecond):
					return
				}
			}
		}
		return
	}

	defer func() {
		if this.FsFs.fareOutChan != nil {
			this.FsFs.fareOutChan <- this.FsFs.Fares.Fs[this.Day][0]
		}
		if this.FsFs.fareInChan != nil {
			this.FsFs.fareInChan <- this.FsFs.Fares.Fs[this.Day][1]
		}
		if this.FsFs.flightOutChan != nil {
			this.FsFs.flightOutChan <- this.FsFs.Flights.Fs[this.Day][0]
		}
		if this.FsFs.flightInChan != nil {
			this.FsFs.flightInChan <- this.FsFs.Flights.Fs[this.Day][1]
		}
	}()

	if this.Agent.SaveAs == "Cache" {
		this.FsFs.Cache()
	} else {


		this.FsFs.KeyCache()
	}
}



func (this *sProvider) ShoppingOut() {
	listMB := this.FsFs.Fares.Fs[this.Day][0]
	if len(this.FsFs.Fares.Fs[this.Day]) > 1 {
		listMB = append(listMB, this.FsFs.Fares.Fs[this.Day][1]...)
	}

	//fmt.Println("listMB Len=", len(listMB), "this.day", this.Day, len(this.FsFs.Flights.Fs[this.Day][0].Route), len(this.FsFs.Flights.Fs[this.Day][1].Route))
	shopping := ToShopping_V2(this.P2pi, this.rout,
		mysqlop.ResultMainBill{listMB}, //这里主要是被引用票单ID,可以不要.
		this.theTrip, this.thePrefixSuffix,
		this.FsFs.Fares.Ris)
	//fmt.Println("shopping Len=", len(shopping.Segment), len(shopping.Fare), len(shopping.Journey))
	if this.agentLen == 1 { //2在合并后处理结果
		MergeSegmentFlight(shopping)
	}

	//fmt.Println("Journey", this.SourceAgency, len(shopping.Journey))
	this.dealOut <- &sPipe{
		Agency: this.SourceAgency,
		Day:    this.Day,
		Index:  this.DealID % 4,
		Info:   shopping,
	}
}

//#TODO 在这里处理了 导致了  keycache
func (this *sProvider) MakeShopping() {

	this.ReadFsFs()

	if this.P2pi.Quick && this.dealOut == nil {
		return //组合多余的天数,不用再处理(只有FB才有机会多出天数)
	}
	this.FsFs.Fares.Calssify(false)
	this.FsFs.Flights.Matching()
	this.FsFs.MatchingLegs()

	if !this.P2pi.Quick && this.Agent.Agency == "FB" {
		this.FsFs.Fares.MarkNoget()
		this.FsFs.GetDynFlight()
		if this.dealOut == nil {
			return
		}
		this.FsFs.MatchingLegs()
	}

	if this.Agent.Agency == "FB" {
		this.FsFs.Fares.MergeMiddleMatch()
	}

	this.FsFs.Fares.Calssify(true)
	this.ShoppingOut()
}



func (this *sShopping) Init() {
	this.dealIn = make(chan *sPipe, this.P2pi.Days)
	this.ticketCount = 1

	var use [2]map[string]struct{}
	addDays, addInDay, addOutDay := 0, 0, 0

	if len(this.Agents) == 2 {
		use[0], use[1] = splitUseRoutine(this.sourceRoutine)
		addDays, addInDay, addOutDay, this.ticketCount = TwoTicketConnDay(this.sourceRoutine[0], this.P2pi)
		//fmt.Println("addDays, addInDay", addDays, addInDay, addOutDay, this.ticketCount)
	}

	P2pi := *this.P2pi //多天的非FB都必须Quick=True
	P2pi.Quick = true

	for ai, agent := range this.Agents {
		totalDays := this.P2pi.Days
		if agent.SaveAs == "Cache" {
			totalDays += addDays //这里多出的一天不一定有效
		}
		fares := make([][]mysqlop.ListMainBill, totalDays)
		flights := make([][]cacheflight.FlightJSON, totalDays)
		fsDyn := make([][]cacheflight.FlightJSON, totalDays)
		fareOutChan := make([]chan mysqlop.ListMainBill, totalDays)
		fareInChan := make([]chan mysqlop.ListMainBill, totalDays)
		flightOutChan := make([]chan cacheflight.FlightJSON, totalDays)
		flightInChan := make([]chan cacheflight.FlightJSON, totalDays)
		flightDynOutChan := make([]chan cacheflight.FlightJSON, totalDays)
		flightDynInChan := make([]chan cacheflight.FlightJSON, totalDays)
		mixModel := false
		var synchronous chan struct{}
		if agent.SaveAs == "Cache" { //2票中的Cache都适合的
			if totalDays > 1 { //同步动态航班数据
				synchronous = make(chan struct{}, totalDays-1)
			}
			if this.ticketCount == 4 {
				mixModel = true
				for day := 0; day < totalDays; day++ {
					if (ai == 0 && day < this.P2pi.Days) || //LegOne时,addDays输出不共享.
						(ai == 1 && day >= addDays) { //LegTwo时,OUT(two+1)-->IN(two)
						fareOutChan[day] = make(chan mysqlop.ListMainBill, 1)
						flightOutChan[day] = make(chan cacheflight.FlightJSON, 1)
						if agent.Agency == "FB" { //Cache只有FB才有动态
							flightDynOutChan[day] = make(chan cacheflight.FlightJSON, 1)
						}
					}
					if ((ai == 0 && day >= addDays) || //LegOne时,OUT(one)-->IN(one+1)
						(ai == 1 && day < this.P2pi.Days)) && //LegTwo时,addDays输出不共享.
						len(this.P2pi.Flight) > 1 { //必须双程
						fareInChan[day] = make(chan mysqlop.ListMainBill, 1)
						flightInChan[day] = make(chan cacheflight.FlightJSON, 1)
						if agent.Agency == "FB" {
							flightDynInChan[day] = make(chan cacheflight.FlightJSON, 1)
						}
					}
				}
			}
		}

		var rout []*mysqlop.Routine
		var queryRout [][2][]string
		if len(this.sourceRoutine) == 0 { //单供应商模式
			if this.Agents[0].SaveAs == "Cache" || this.Agents[0].Agency == "SearchOne" {
				queryRout = [][2][]string{[2][]string{this.P2pi.Rout[0].DepartCounty, this.P2pi.Rout[0].ArriveCounty}}
			} else { //必须单独传城市的,因为这样的远程查询必须使用单一的城市代码
				queryRout = [][2][]string{[2][]string{{this.P2pi.Flight[0].DepartStation}, {this.P2pi.Flight[0].ArriveStation}}}
			}
			rout = this.P2pi.Rout
		} else { //组合供应商模式(进入这里前是有组合路线判断的)
			var dc, ac []string
			queryRout, dc, ac = getQueryRoutine(use[ai])
			rout = make([]*mysqlop.Routine, len(this.P2pi.Rout))
			for i := 0; i < len(this.P2pi.Rout); i++ {
				r := *this.P2pi.Rout[i]
				if i == 0 {
					r.DepartCounty = dc
					r.ArriveCounty = ac
				} else {
					r.DepartCounty = ac
					r.ArriveCounty = dc
				}
				rout[i] = &r
			}
		}

		F_provider := func(day int) *sProvider {
			p := &sProvider{
				SourceAgency: this.SourceAgency,
				Agent:        agent,
				P2pi:         this.P2pi,
				Day:          day,
				DealID:       day*4 + ai*2,

				rout:        rout,
				queryRout:   queryRout,
				useRoutine:  use[ai],
				agentLen:    len(this.Agents),
				synchronous: synchronous,
			}

			if len(this.Agents) > 1 {
				if ai == 0 {
					p.addInDay = addInDay
					p.thePrefixSuffix = true
				} else {
					p.addOutDay = addOutDay
					p.theTrip = true
				}
			}

			if !this.P2pi.Quick && this.P2pi.Days > 1 && this.SourceAgency != "FB" {
				p.P2pi = &P2pi
			}

			if agent.SaveAs == "Cache" {
				p.addDays = addDays
			}

			p.FsFs = &sFaresFlights{
				Fares: &sFares{
					Fs: fares,
				},
				Flights: &sFlights{
					Fs:    flights,
					FsDyn: fsDyn,
				},
				p: p,
			}

			p.FsFs.Fares.p = p.FsFs
			p.FsFs.Flights.p = p.FsFs
			return p
		}

		for day := 0; day < totalDays; day++ {
			p := F_provider(day)
			p.FsFs.fareOutChan = fareOutChan[day]
			p.FsFs.fareInChan = fareInChan[day]
			p.FsFs.flightOutChan = flightOutChan[day]
			p.FsFs.flightInChan = flightInChan[day]
			p.FsFs.flightDynOutChan = flightDynOutChan[day]
			p.FsFs.flightDynInChan = flightDynInChan[day]
			if day < this.P2pi.Days { //有时候多出一天作为2票(但数据只是中途使用)
				p.dealOut = this.dealIn
			}

			go p.MakeShopping()
		}

		if this.ticketCount < 4 {
			continue
		}

		fares = make([][]mysqlop.ListMainBill, this.P2pi.Days)
		flights = make([][]cacheflight.FlightJSON, this.P2pi.Days)
		if agent.Agency == "FB" {
			fsDyn = make([][]cacheflight.FlightJSON, this.P2pi.Days)
		}

		for day := 0; day < this.P2pi.Days; day++ { //第2,4次是不用循环addDays的,因为增加的天数没有接受者.
			p := F_provider(day)
			p.DealID++
			p.mixModel = mixModel
			if ai == 0 { //chan的值被Cache使用,addDays被KeyCache使用.
				p.addInDay++
				p.FsFs.fareOutChan = fareOutChan[day]
				p.FsFs.fareInChan = fareInChan[day+addDays]
				p.FsFs.flightOutChan = flightOutChan[day]
				p.FsFs.flightInChan = flightInChan[day+addDays]
				p.FsFs.flightDynOutChan = flightDynOutChan[day]
				p.FsFs.flightDynInChan = flightDynInChan[day+addDays]
			} else {
				p.addOutDay++
				p.FsFs.fareOutChan = fareOutChan[day+addDays]
				p.FsFs.fareInChan = fareInChan[day]
				p.FsFs.flightOutChan = flightOutChan[day+addDays]
				p.FsFs.flightInChan = flightInChan[day]
				p.FsFs.flightDynOutChan = flightDynOutChan[day+addDays]
				p.FsFs.flightDynInChan = flightDynInChan[day]
			}
			p.dealOut = this.dealIn
			go p.MakeShopping()
		}
	}
}


func (this *sShopping) MakeShopping() {
	this.Init()

	count := this.P2pi.Days
	if this.ticketCount == 1 {
		for i := 0; i < count; i++ {
			this.dealOut <- <-this.dealIn
		}
	} else {
		count = count * this.ticketCount
		shopping := make([][4]*Point2Point_Output, this.P2pi.Days)
		done := make(map[int]struct{}, this.P2pi.Days)

		if this.ticketCount == 2 {
			for i := 0; i < this.P2pi.Days; i++ {
				shopping[i][1] = nullOutput
				shopping[i][3] = nullOutput
			}
		}

		for i := 0; i < count; i++ {
			pipe := <-this.dealIn
			shopping[pipe.Day][pipe.Index] = pipe.Info.(*Point2Point_Output)

			if shopping[pipe.Day][0] != nil &&
				shopping[pipe.Day][1] != nil &&
				shopping[pipe.Day][2] != nil &&
				shopping[pipe.Day][3] != nil {

				done[pipe.Day] = struct{}{}
				shopping := MergeJourney_V2(this.P2pi,
					shopping[pipe.Day][0],
					shopping[pipe.Day][1],
					shopping[pipe.Day][2],
					shopping[pipe.Day][3])
				MergeSegmentFlight(shopping)
				this.dealOut <- &sPipe{
					Agency: this.SourceAgency,
					Day:    pipe.Day,
					Info:   shopping,
				}
			}
		}

		for day := 0; day < this.P2pi.Days; day++ {
			if _, ok := done[day]; !ok {
				this.dealOut <- &sPipe{ //不会来到这里的,简单处理安全.
					Agency: this.SourceAgency,
					Day:    day,
					Info: &Point2Point_Output{
						Result:  2,
						Segment: []*Point2Point_RoutineInfo2Segment{},
						Fare:    []*FareInfo{},
						Journey: []*JourneyInfo{}},
				}
			}
		}
	}
}

func (this *sProdution) Init() int {
	//this.P2pi.Rout = FlightLegs2Routine(this.P2pi.Flight)
	mds := getProvider(this.P2pi.Offices)
	this.dealIn = make(chan *sPipe, len(mds))
	iagencis := getAllAgencis()
	for _, agent := range iagencis {
		if this.P2pi.Agency != "" && this.P2pi.Agency != agent.AgencyCode() {
			continue //不是指定的供应商
		}

		if len(this.P2pi.Rout) != 2 && len(agent.Agencis()) == 2 {
			continue //单程不出2票
		}

		if _, ok := mds[agent.AgencyCode()]; !ok {
			continue //不是需要的供应商
		}

		routine := useRoutine(agent.UseRoutine(), this.P2pi.Rout[0])

		if len(routine) == 0 && len(agent.Agencis()) == 2 {
			continue //没有指定路线的直接返回
		}

		shopping := &sShopping{
			SourceAgency:  agent.AgencyCode(),
			Agents:        agent.Agencis(),
			P2pi:          this.P2pi,
			sourceRoutine: routine,
			dealOut:       this.dealIn,
		}

		this.totalCount++

		go shopping.MakeShopping()
	}

	return this.totalCount
}

func (this *sProdution) MakeShopping() {
	outcount := 0 //已经输出的次数
	listoutput := make([]ListPoint2Point_Output, this.totalCount)
	for i := 0; i < this.totalCount; i++ {
		listoutput[i].ListShopping = make([]*Point2Point_Output, this.P2pi.Days)
	}

	for i := 0; i < this.totalCount*this.P2pi.Days; i++ {
		select {
		case <-time.After(waitSecond + time.Second/2):
			break
		case pipe := <-this.dealIn:
			for j := outcount; j < this.totalCount; j++ {
				if listoutput[j].ListShopping[pipe.Day] == nil {
					listoutput[j].ListShopping[pipe.Day] = pipe.Info.(*Point2Point_Output)
				} else {
					continue
				}

				if this.P2pi.Quick { //快速Shopping一起返回
					break
				}

				day := 0
				for ; day < this.P2pi.Days; day++ {
					if listoutput[j].ListShopping[day] == nil {
						break
					}
				}

				if day == this.P2pi.Days {
					//fmt.Println("this.dealOut <- listoutput", outcount)
					this.dealOut <- listoutput[outcount]
					outcount++
				}

				break
			}
		} //end select
	}

	if this.P2pi.Quick { //sShopping合并多供应商,sProduction合并Quick处理.
		for day := 0; day < this.P2pi.Days; day++ {
			for i := 1; i < this.totalCount; i++ {
				if listoutput[i].ListShopping[day] != nil &&
					len(listoutput[i].ListShopping[day].Journey) > 0 {

					listoutput[0].ListShopping[day].Segment =
						append(listoutput[0].ListShopping[day].Segment,
							listoutput[i].ListShopping[day].Segment...)

					listoutput[0].ListShopping[day].Fare =
						append(listoutput[0].ListShopping[day].Fare,
							listoutput[i].ListShopping[day].Fare...)

					listoutput[0].ListShopping[day].Journey =
						append(listoutput[0].ListShopping[day].Journey,
							listoutput[i].ListShopping[day].Journey...)
				}
			}

			if listoutput[0].ListShopping[day] != nil {
				sort.Sort(listoutput[0].ListShopping[day].Journey)
				if this.P2pi.MaxOutput > 100 && len(listoutput[0].ListShopping[day].Journey) > this.P2pi.MaxOutput {
					listoutput[0].ListShopping[day].Journey = listoutput[0].ListShopping[day].Journey[:this.P2pi.MaxOutput]
				}
				MergeSegmentFlight(listoutput[0].ListShopping[day])
			}
		}
		this.dealOut <- listoutput[0]
	} else {
		for ; outcount < this.totalCount; outcount++ { //最后补充,这步不会执行到,只是为了安全起见.
			this.dealOut <- listoutput[outcount]
		}
	}
}


func WAPI_QuickPoint2Point_V2(w http.ResponseWriter, r *http.Request) {

	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var p2pi Point2Point_In
	if err := json.Unmarshal(result, &p2pi); err != nil {
		errorlog.WriteErrorLog("WAPI_QuickPoint2Point_V2 (1): " + err.Error())
		fmt.Fprint(w, bytes.NewBuffer(ListShoppingErr))
		return
	}

	if len(p2pi.Flight) == 0 {
		fmt.Fprint(w, bytes.NewBuffer(ListShoppingErr))
		return
	}

	reduceSpace := func(dest string) string {
		if dest == "" {
			return dest
		}

		stations := strings.Split(dest, " ")
		Len := len(stations)
		for i := 0; i < Len; {
			if len(stations[i]) == 0 {
				Len--
				stations[i], stations[Len] = stations[Len], stations[i]
			} else {
				i++
			}
		}

		return strings.Join(stations[:Len], " ")
	}

	for i := range p2pi.Flight {
		p2pi.Flight[i].DepartStation = reduceSpace(p2pi.Flight[i].DepartStation)
		p2pi.Flight[i].ConnectStation = reduceSpace(p2pi.Flight[i].ConnectStation)
		p2pi.Flight[i].ArriveStation = reduceSpace(p2pi.Flight[i].ArriveStation)
	}

	//下面的写法是怕有的人录入的方式把单程录程双程,但系统解析到单程.
	p2pi.Rout = FlightLegs2Routine(p2pi.Flight)
	routLen := len(p2pi.Rout)

	if routLen > 2 || routLen == 0 || p2pi.Days < 0 || p2pi.Days > 60 {
		fmt.Fprint(w, bytes.NewBuffer(ListShoppingErr))
		return
	}

	lenDepartStation := len(strings.Split(p2pi.Flight[0].DepartStation, " "))
	lenArriveStation := len(strings.Split(p2pi.Flight[0].ArriveStation, " "))

	if p2pi.Days == 0 || lenDepartStation > 1 || lenArriveStation > 1 {
		p2pi.Days = 1
	}

	if p2pi.Days > 1 || lenDepartStation > 1 || lenArriveStation > 1 {
		p2pi.Debug = false //日历功能禁止调试
	}

	//出发地与目的地间只存在一个"多",这是排版的需要.
	if lenDepartStation > 1 && lenArriveStation > 1 {
		fmt.Fprint(w, bytes.NewBuffer(ListShoppingErr))
		return
	}

	if lenDepartStation > 5 || lenArriveStation > 5 {
		fmt.Fprint(w, bytes.NewBuffer(ListShoppingErr))
		return
	}

	if routLen == 2 && p2pi.Rout[0].Trip == 0 { //目前不操作缺口程数据
		fmt.Fprint(w, bytes.NewBuffer(ListShoppingErr))
		return
	}

	if len(p2pi.Offices) == 0 {
		p2pi.Offices = []string{"3007F"}
	}

	if p2pi.UserKind1 == "" {
		p2pi.UserKind1 = "INCU"
	}

	if p2pi.Agency != "" {
		iagencis := getAllAgencis()
		la := 0
		for ; la < len(iagencis); la++ {
			if iagencis[la].AgencyCode() == p2pi.Agency {
				break
			}
		}
		if la == len(iagencis) {
			fmt.Fprint(w, bytes.NewBuffer(ListShoppingErr))
			return
		}
	}

	if p2pi.ConnMinutes < 20 {
		p2pi.ConnMinutes = 20
	} else if p2pi.ConnMinutes > 1440 {
		p2pi.ConnMinutes = 1440
	}

	if p2pi.DeviationRate < 100 {
		p2pi.DeviationRate = 100
	} else if p2pi.DeviationRate > 250 {
		p2pi.DeviationRate = 250
	}

	/***航司联盟控制2016-02-15***/
	if index := mysqlop.AllianceIndex(p2pi.Alliance); index < 99 && len(p2pi.Airline) == 0 {
		p2pi.Airline = cachestation.AllianceList[index]
	}

	if p2pi.GetCount <= 0 {
		p2pi.GetCount = 1
	}

	journeyline := strconv.Itoa(p2pi.Days) + "/" + JourneyLine(&p2pi)
	if p2pi.Quick {
		journeyline = "Q/" + journeyline
	}

	var dc *DebugControl
	if p2pi.Debug {
		dc = &DebugControl{}
		dc.Init(journeyline)
	}

	AgentsShoppingOut.mutex.RLock()
	aso, b := AgentsShoppingOut.data[journeyline]
	AgentsShoppingOut.mutex.RUnlock()

	//网络输出
	wait := &sWait{
		GetCount: p2pi.GetCount,
		Shopping: make(chan []byte, 1),
	}

	//shopping输出供应商,接口缓存供应商多次输出(缓存对象)
	createOA := func(AgentLen int) *sOutAgents {
		oa := &sOutAgents{
			AirAgents: AgentLen,
			Days:      p2pi.Days,
			time:      time.Now(),
			dealIn:    make(chan ListPoint2Point_Output, AgentLen),
			ListWait:  []*sWait{wait},
		}

		if p2pi.Quick {
			oa.AirAgents = lenDepartStation * lenArriveStation //热门旅游,每一地点返回一次
		}

		oa.ListShopping = make([][]byte, oa.AirAgents)

		AgentsShoppingOut.mutex.Lock()
		AgentsShoppingOut.data[journeyline] = oa
		AgentsShoppingOut.mutex.Unlock()

		go func() {
			time.Sleep(time.Minute * 3)
			AgentsShoppingOut.mutex.Lock()
			delete(AgentsShoppingOut.data, journeyline)
			AgentsShoppingOut.mutex.Unlock()
		}()

		return oa
	}

	//shopping输出供应商,接口缓存供应商多次输出
	outAgent := func(oa *sOutAgents) {
		for i := 0; i < oa.AirAgents; i++ {
			lppo := <-oa.dealIn

			if i+1 != oa.AirAgents {
				notJump := false
				for day := 0; day < p2pi.Days; day++ {
					if lppo.ListShopping[day] != nil && len(lppo.ListShopping[day].Journey) > 0 {
						notJump = true
						break
					}
				}
				if !notJump {
					oa.AirAgents--
					i--
					continue
				}
			} else {
				for day := 0; day < p2pi.Days; day++ {
					if lppo.ListShopping[day] != nil && lppo.ListShopping[day].Result == 2 {
						lppo.ListShopping[day].Result = 1
					}
				}
			}

			//oa.ListShopping[oa.Count] = lppo
			oa.ListShopping[oa.Count] = errorlog.Make_JSON_GZip(lppo)
			oa.Count++

			//fmt.Println("Shopping Out", oa.Count, "JourneyLen", len(oa.ListShopping[oa.Count-1].ListShopping[0].Journey))

			oa.mutex.Lock()
			LenWait := len(oa.ListWait)
			for j := 0; j < LenWait; {
				if oa.Count == oa.AirAgents || //最后一次要把等待处理完
					oa.ListWait[j].GetCount == oa.Count {
					oa.ListWait[j].Shopping <- oa.ListShopping[oa.Count-1]
					LenWait--
					oa.ListWait[j], oa.ListWait[LenWait] = oa.ListWait[LenWait], oa.ListWait[j]
				} else {
					j++
				}
			}
			oa.ListWait = oa.ListWait[:LenWait]
			oa.mutex.Unlock()
		}
	}

	if b {
		if aso.AirAgents < p2pi.GetCount {
			fmt.Fprint(w, bytes.NewBuffer(ListShoppingEnd))
			return
		}

		if aso.Count >= p2pi.GetCount {
			fmt.Fprint(w, bytes.NewBuffer(aso.ListShopping[p2pi.GetCount-1]))
			return
		}

		aso.mutex.Lock()
		aso.ListWait = append(aso.ListWait, wait)
		aso.mutex.Unlock()

	} else if lenDepartStation > 1 || lenArriveStation > 1 {
		wait.GetCount = 1 //初次必须是1
		AgentLen := 0
		ps := make([]*sProdution, 0, lenDepartStation*lenArriveStation)

		for _, depart := range strings.Split(p2pi.Flight[0].DepartStation, " ") {
			for _, arrive := range strings.Split(p2pi.Flight[0].ArriveStation, " ") {
				P2pi := p2pi
				for fi := range p2pi.Flight {
					r := *p2pi.Flight[fi]
					if fi == 0 {
						r.DepartStation = depart
						r.ArriveStation = arrive
					} else {
						r.DepartStation = arrive
						r.ArriveStation = depart
					}
					P2pi.Flight[fi] = &r
				}

				p := &sProdution{
					P2pi: &P2pi,
				}

				al := p.Init()
				if al > 0 {
					AgentLen += al
					ps = append(ps, p)
				}
			}
		}
		if AgentLen == 0 {
			fmt.Fprint(w, bytes.NewBuffer(ListShoppingEnd))
			return
		}

		oa := createOA(AgentLen)
		for _, p := range ps {
			p.dealOut = oa.dealIn
			go p.MakeShopping()
		}
		go outAgent(oa)
	} else {
		wait.GetCount = 1 //初次必须是1

		p := &sProdution{
			P2pi: &p2pi,
		}
		AgentLen := p.Init()
		if AgentLen == 0 {
			fmt.Fprint(w, bytes.NewBuffer(ListShoppingEnd))
			return
		}

		oa := createOA(AgentLen)
		p.dealOut = oa.dealIn

		go p.MakeShopping()
		go outAgent(oa)
	}

	select {
	case out := <-wait.Shopping:
		fmt.Fprint(w, bytes.NewBuffer(out))
	case <-time.After(waitSecond + time.Second):
		fmt.Fprint(w, bytes.NewBuffer(ShoppingTimeOut))
	}
}
