package webapi

import (

	"bytes"
	"cacheflight"
	//"compress/gzip"
	"encoding/json"
	"errorlog"
	"fmt"
	"io/ioutil"
	"net/http"
	//"outsideapi"
	"sort"
	"strconv"
	//"strings"
	"sync"
	"cachestation"
	"mysqlop"
	"time"
)




type FlightInfo_In struct {
	Airline       string                `json:"Airline"`       //航空公司
	Alliance      string                `json:"Alliance"`      //航空联盟名称,空时输入""例如oneworld,skyteam,starAlliance
	DeviationRate string                `json:"DeviationRate"` //绕航率(默认不传的话"150")    210
	ConnMinutes   string                `json:"ConnMinutes"`   //中转总时间,单位分钟(默认为"300")   1440
	ShareAirline  string                `json:"ShareAirline"`  //默认会认为B，显示所有 是否需要共享航班数据 A(不共享--相当过滤共享数据) B(共享 默认值--相当全部数据出来)
	NoConnect     string                `json:"NoConnect"`     // （默认为0，也就是输出所有） 0(笼统输出按1-->2-->3的次序获取到RoutineCount的要求) 1(直飞数据) 2(1次转机数据) 3(2次转机数据)   0
	MixCount      string                `json:"MixCount"`      //触发混航司的记录数   20  （如果数据多的情况，多点无所谓；如果数据很少，则去混合一些别的航司把数据凑上去）
	RoutineCount  string                `json:"RoutineCount"`  //返回路线数量   50
	Legs          []*Point2Point_Flight `json:"Legs"`   //这里的Legs应该就是要输入段的意思了（其实就是行程）
	/**
	在这里表明Point2Point_Flight 结构里面的内容，方便查看
	type Point2Point_Flight struct {
	DepartStation  string `json:"DepartStation"`   //出发地机场  XIY
	ConnectStation string `json:"ConnectStation"` //中转地机场  CAN
	ArriveStation  string `json:"ArriveStation"` //目的地机场  JHB
	TravelDate     string `json:"TravelDate"`  //旅行日期  2018-09-08
}
	*/
}


//最终输出的航班数据也是按照这种类型输出。
type FlightInfo_Output struct {
	Route []*FlightInfo `json:"Route"`
}

/****出发地与目的地接驳，可飞路线接口(WAPI_MatchingFlight_V* 服务系列)****/
type FlightInfo struct {
	Segment             string                          `json:"SC"`
	TravelDate          string                          `json:"DSD"`
	DeviationRate       int                             `json:"DR"`
	TotleMile           int                             `json:"TM"`
	DepartureStation    string                          `json:"DS"`
	ArrivalStation      string                          `json:"AS"`
	AirlineDesignator   string                          `json:"AD"`
	Routine             string                          `json:"R"`
	FlightNumber        string                          `json:"RFN"`
	TripType            string                          `json:"TT"`   //11 12  21 22 31
	TransferType        string                          `json:"-"`
	MinutesOfTrip       int                             `json:"TMs"`
	DaysOfTrip          int                             `json:"TDs"`
	MinutesOfConnecting int                             `json:"CMs"`
	DaysOfConnecting    int                             `json:"CDs"`
	DepartureTime       string                          `json:"DST"`
	DepartureUTC        string                          `json:"DUTC"`
	ArrivalTime         string                          `json:"AST"`
	ArrivalUTC          string                          `json:"AUTC"`
	CodeShare           string                          `json:"CS"`
	ShareAirline        string                          `json:"SA"`
	Legs                []*cacheflight.FlightInfoStruct `json:"LegInfo"`

}


//这个是返回错误信息，或者是找不到信息的代码
var FlightInfoErrOut []byte


var MixAirlineNum int = 20 //默认混航司触发记录数


//中转地缓存
var ConnectStationCache struct {
	ConnectStation map[string][]string //string1 = Depart + Arrive + IntelligentConnect_V*
	mutex          sync.RWMutex
}






//这里是制作航班的表头信息
func Copy2FlightInfo(Segment string, DeviationRate int,
	ris *cacheflight.RoutineInfoStruct) *FlightInfo {

	legslen := len(ris.FI[0].Legs)

	fi := &FlightInfo{
		Segment:             Segment,
		TravelDate:          ris.FI[0].Legs[0].DSD,
		DeviationRate:       DeviationRate,
		TotleMile:           ris.TM,
		DepartureStation:    ris.FI[0].Legs[0].DS,
		ArrivalStation:      ris.FI[0].Legs[legslen-1].AS,
		AirlineDesignator:   ris.FI[0].Legs[0].AD,
		Routine:             ris.R,
		FlightNumber:        ris.RFN,
		TripType:            ris.TT,
		MinutesOfTrip:       ris.TMs,
		DaysOfTrip:          ris.TDs,
		MinutesOfConnecting: ris.CMs,
		DaysOfConnecting:    ris.CDs,
		DepartureTime:       ris.FI[0].Legs[0].DST,
		DepartureUTC:        ris.FI[0].Legs[0].DUTC,
		ArrivalTime:         ris.FI[0].Legs[legslen-1].AST,
		ArrivalUTC:          ris.FI[0].Legs[legslen-1].AUTC,
		CodeShare:           ris.CS,
		ShareAirline:        ris.SA,
		Legs:                ris.FI[0].Legs,
	}

	//这里就是返回11，12，21，22，31，32那种；也就是多航司，多航段。主要是通过传入fi这个数据进去。接着再去里面自动处理Triptype
	fi.GetTripType()

	return fi
}


//#TODO   Copy2FlightInfo_V2 目前没有其他地方使用到，是否为了兼顾新需求
func Copy2FlightInfo_V2(Segment string,
	CDs int, CMs int, subRate int,
	rout_fore *cacheflight.RoutineInfoStruct,
	rout_back *FlightInfo) *FlightInfo {

	backlength := len(rout_back.Legs)

	var fi *FlightInfo = &FlightInfo{
		Segment:             Segment,
		TravelDate:          rout_fore.FI[0].Legs[0].DSD,
		DeviationRate:       subRate,
		TotleMile:           rout_fore.TM + rout_back.TotleMile,
		DepartureStation:    rout_fore.FI[0].Legs[0].DS,
		ArrivalStation:      rout_back.Legs[backlength-1].AS,
		AirlineDesignator:   rout_fore.FI[0].Legs[0].AD,
		Routine:             rout_fore.R + rout_back.Routine[3:],
		FlightNumber:        rout_fore.RFN + rout_back.FlightNumber,
		TripType:            "E",
		MinutesOfTrip:       rout_fore.TMs + rout_back.MinutesOfTrip + CMs,
		DaysOfTrip:          rout_fore.TDs + rout_back.DaysOfTrip + CDs,
		MinutesOfConnecting: rout_fore.CMs + rout_back.MinutesOfConnecting + CMs,
		DaysOfConnecting:    rout_fore.CDs + rout_back.DaysOfConnecting + CDs,
		DepartureTime:       rout_fore.FI[0].Legs[0].DST,
		DepartureUTC:        rout_fore.FI[0].Legs[0].DUTC,
		ArrivalTime:         rout_back.Legs[backlength-1].AST,
		ArrivalUTC:          rout_back.Legs[backlength-1].AUTC,
		CodeShare:           "",
		ShareAirline:        "A",
		Legs:                make([]*cacheflight.FlightInfoStruct, 0, 3)}

	if rout_fore.CS != "" {
		fi.CodeShare = rout_fore.CS
	} else if rout_back.CodeShare != "" {
		fi.CodeShare = rout_back.CodeShare
	}

	if rout_fore.SA == "B" || rout_back.ShareAirline == "B" {
		fi.ShareAirline = "B"
	} else {
		fi.ShareAirline = "A"
	}

	fi.Legs = append(append(fi.Legs, rout_fore.FI[0].Legs...), rout_back.Legs...)

	fi.GetTripType()

	return fi
}



/**
这里传入了去程的航班信息，回程的航班信息。综合起来。最后合并一起出去
*/
func Copy2FlightInfo_V3(Segment string, DeviationRate int, CDs int, CMs int,
	rout_fore *cacheflight.RoutineInfoStruct,
	rout_back *cacheflight.RoutineInfoStruct) *FlightInfo {

	backlength := len(rout_back.FI[0].Legs)

	var fi *FlightInfo = &FlightInfo{
		Segment:             Segment,
		TravelDate:          rout_fore.FI[0].Legs[0].DSD,
		DeviationRate:       DeviationRate,
		TotleMile:           rout_fore.TM + rout_back.TM,
		DepartureStation:    rout_fore.FI[0].Legs[0].DS,
		ArrivalStation:      rout_back.FI[0].Legs[backlength-1].AS,
		AirlineDesignator:   rout_fore.FI[0].Legs[0].AD,
		Routine:             rout_fore.R + rout_back.R[3:],
		FlightNumber:        rout_fore.RFN + rout_back.RFN,
		TripType:            "E",
		MinutesOfTrip:       rout_fore.TMs + rout_back.TMs + CMs,
		DaysOfTrip:          rout_fore.TDs + rout_back.TDs + CDs,
		MinutesOfConnecting: rout_fore.CMs + rout_back.CMs + CMs,
		DaysOfConnecting:    rout_fore.CDs + rout_back.CDs + CDs,
		DepartureTime:       rout_fore.FI[0].Legs[0].DST,
		DepartureUTC:        rout_fore.FI[0].Legs[0].DUTC,
		ArrivalTime:         rout_back.FI[0].Legs[backlength-1].AST,
		ArrivalUTC:          rout_back.FI[0].Legs[backlength-1].AUTC,
		CodeShare:           "",
		ShareAirline:        "A",
		Legs:                make([]*cacheflight.FlightInfoStruct, 0, 3),
	}

	if rout_fore.CS != "" {
		fi.CodeShare = rout_fore.CS
	} else if rout_back.CS != "" {
		fi.CodeShare = rout_back.CS
	}

	if rout_fore.SA == "B" || rout_back.SA == "B" {
		fi.ShareAirline = "B"
	} else {
		fi.ShareAirline = "A"
	}

	fi.Legs = append(append(fi.Legs, rout_fore.FI[0].Legs...), rout_back.FI[0].Legs...)

	fi.GetTripType()

	return fi
}

func Copy2FlightInfo_V4(Segment string,
	DeviationRate int, CDs int, CMs int,
	rout_fore *FlightInfo,
	rout_back *FlightInfo) *FlightInfo {

	backlength := len(rout_back.Legs)

	var fi *FlightInfo = &FlightInfo{
		Segment:             Segment,
		TravelDate:          rout_fore.Legs[0].DSD,
		DeviationRate:       DeviationRate,
		TotleMile:           rout_fore.TotleMile + rout_back.TotleMile,
		DepartureStation:    rout_fore.Legs[0].DS,
		ArrivalStation:      rout_back.Legs[backlength-1].AS,
		AirlineDesignator:   rout_fore.Legs[0].AD,
		Routine:             rout_fore.Routine + rout_back.Routine[3:],
		FlightNumber:        rout_fore.FlightNumber + rout_back.FlightNumber,
		TripType:            "E",
		MinutesOfTrip:       rout_fore.MinutesOfTrip + rout_back.MinutesOfTrip + CMs,
		DaysOfTrip:          rout_fore.DaysOfTrip + rout_back.DaysOfTrip + CDs,
		MinutesOfConnecting: rout_fore.MinutesOfConnecting + rout_back.MinutesOfConnecting + CMs,
		DaysOfConnecting:    rout_fore.DaysOfConnecting + rout_back.DaysOfConnecting + CDs,
		DepartureTime:       rout_fore.Legs[0].DST,
		DepartureUTC:        rout_fore.Legs[0].DUTC,
		ArrivalTime:         rout_back.Legs[backlength-1].AST,
		ArrivalUTC:          rout_back.Legs[backlength-1].AUTC,
		CodeShare:           "",
		ShareAirline:        "A",
		Legs:                make([]*cacheflight.FlightInfoStruct, 0, 3)}

	if rout_fore.CodeShare != "" {
		fi.CodeShare = rout_fore.CodeShare
	} else if rout_back.CodeShare != "" {
		fi.CodeShare = rout_back.CodeShare
	}

	if rout_fore.ShareAirline == "B" || rout_back.ShareAirline == "B" {
		fi.ShareAirline = "B"
	} else {
		fi.ShareAirline = "A"
	}

	fi.Legs = append(append(fi.Legs, rout_fore.Legs...), rout_back.Legs...)

	fi.GetTripType()

	return fi
}




//获取直飞的航班处理。。flightinfo_out 就是为了接收回调的数据的...OneLeg 直飞
func MatchingOneLeg_V1(
	SegmentCode string,
	DepartStation string,
	ArriveStation string,
	TravelDate string,
	ShareAirline string,
	Airline string,
	Alliance string,
	flightinfo_out chan *FlightInfo_Output) {


	flightinfo_output := &FlightInfo_Output{}

	defer func() {
		flightinfo_out <- flightinfo_output
	}()


	c_fore := make(chan *cacheflight.FlightJSON, 1)

	//QueryLegInfo_V3。。这里引入了另外一个函数。将要查询到额参数传到QueryLegInfo_V3里面去。最终将回调的结果存到c_fore里面回来。还根据出发地，传了对应的服务器地址
	//下面的Legs==1，代表我只要直飞的。例如CAN-BKK。不要有中转。如果Legs==2。则代表一次中转
	//
	QueryLegInfo_V3(&cacheflight.RoutineService{
		Deal:           "QueryForeLegsDays",
		DepartStation:  DepartStation,
		ConnectStation: []string{},
		ArriveStation:  ArriveStation,
		TravelDate:     TravelDate,
		Legs:           1,
		Days:           1},
		cachestation.PetchIP[DepartStation], c_fore)


	AllianceIndex := mysqlop.AllianceIndex(Alliance)
	fjson_fore := <-c_fore

	flightinfo_output.Route = make([]*FlightInfo, 0, len(fjson_fore.Route))

	for _, rout_fore := range fjson_fore.Route {
		if ShareAirline == "A" && rout_fore.SA == "B" ||
			Airline != "" && Airline != rout_fore.FI[0].Legs[0].AD ||
			AllianceIndex < 99 && !MatchingAlliance(rout_fore.FI[0].Legs, AllianceIndex) {
			continue
		}


		flightinfo_output.Route = append(flightinfo_output.Route, Copy2FlightInfo(SegmentCode, 100, rout_fore))

	}
}






//#TODO MatchingTwoLeg这里有V1和V2 两种处理流程。也就是用来处理一次转机的流程
//获取一次转机的航班处理(现在只用于2次接驳调用的1次转机,和专门的一次转机业务有差别)
func MatchingTwoLeg_V1(
	SegmentCode string,
	DepartStation string,
	ArriveStation string,
	TravelDate string,
	flightinfo_in FlightInfo_In,
	flightinfo_out chan *FlightInfo_Output) {

	flightinfo_output := &FlightInfo_Output{Route: make([]*FlightInfo, 0, 700)}

	defer func() {
		flightinfo_out <- flightinfo_output
	}()

	TM := cacheflight.DestGetDistance(DepartStation, ArriveStation)

	if TM == 0 {
		return
	}

	DeviationRate, _ := strconv.Atoi(flightinfo_in.DeviationRate)
	ConnMinutes, _ := strconv.Atoi(flightinfo_in.ConnMinutes)
	Airline := flightinfo_in.Airline
	//lenairline := len(Airline)
	Alliance := flightinfo_in.Alliance
	AllianceIndex := mysqlop.AllianceIndex(Alliance)
	ShareAirline := flightinfo_in.ShareAirline
	MixCount, _ := strconv.Atoi(flightinfo_in.MixCount)

	//出发地目的地  例如CANBKK
	legrout := DepartStation + ArriveStation
	if DepartStation > ArriveStation {
		legrout = ArriveStation + DepartStation
	}


	c_twoleg := make(chan *cacheflight.FlightJSON, 1)
	go QueryLegInfo_V3(&cacheflight.RoutineService{
		Deal:           "QueryForeLegsDays",
		DepartStation:  DepartStation,
		ConnectStation: []string{},
		ArriveStation:  ArriveStation,
		TravelDate:     TravelDate,
		Legs:           2,
		Days:           1},
		cachestation.PetchIP[DepartStation], c_twoleg)


	c_fore := make(chan *cacheflight.FlightJSON, 1)
	go QueryLegInfo_V3(&cacheflight.RoutineService{
		Deal:           "QueryForeLegsDays",
		DepartStation:  DepartStation,
		ConnectStation: cachestation.Routine[legrout],
		ArriveStation:  "***", //ArriveStation,
		TravelDate:     TravelDate,
		Legs:           1,
		Days:           1},
		cachestation.PetchIP[DepartStation], c_fore)

	c_back := make(chan *cacheflight.FlightJSON, 1)
	go QueryLegInfo_V3(&cacheflight.RoutineService{
		Deal:           "QueryBackLegsDays",
		DepartStation:  "***", //DepartStation,
		ConnectStation: cachestation.Routine[legrout],
		ArriveStation:  ArriveStation,
		TravelDate:     TravelDate,
		Legs:           1,
		Days:           2},
		cachestation.PetchIP[ArriveStation], c_back)

	secondDo := false   //不同日期的匹配
	mixAirline := false //不同航司的接驳 (1)相同航司/日期接驳;(2)相同航司不同日期接驳;(3)不同航司相同日期接驳

	date, _ := time.Parse("2006-01-02", TravelDate)
	beforedate := date.AddDate(0, 0, -1).Format("2006-01-02") //获取前一天数据
	afterdate := date.AddDate(0, 0, 2).Format("2006-01-02")   //获取后天数据

	//以下是复杂业务的变量
	mustGet := make(map[string]string, 50) //直飞未匹配到航班的 string(1)=Routine+FlightNumber,string(2)=TravelDate
	nextFore := &cacheflight.FlightJSON{
		Route: make([]*cacheflight.RoutineInfoStruct, 0, 100)} //登记直飞接驳不到的航班
	var the1_fjson_fore *cacheflight.FlightJSON //用于保存TravelDate数据,以备不同航司接驳使用
	var the1_fjson_back *cacheflight.FlightJSON //用于保存TravelDate数据,以备不同航司接驳使用

	repeatRoutine := make(map[string]struct{}, 150)

	RepeatRoutineCheck := func(routine, flightNumber string) bool {
		_, ok := repeatRoutine[routine+flightNumber]
		return ok
	}

	fjson_twoleg := ReduceRout(<-c_twoleg, ShareAirline, Airline, AllianceIndex, true)

	//直接中转存在的数据
	for _, rout_fore := range fjson_twoleg.Route { //这里非混航司的,要求同航司
		if rout_fore.FI[0].Legs[0].AD != rout_fore.FI[0].Legs[1].AD {
			continue
		}

		flightinfo_output.Route = append(flightinfo_output.Route,
			Copy2FlightInfo(SegmentCode, rout_fore.DR, rout_fore))

		repeatRoutine[rout_fore.R+rout_fore.RFN] = struct{}{}
	}

	fjson_fore := ReduceRout(<-c_fore, ShareAirline, Airline, AllianceIndex, false)
	fjson_back := ReduceRout(<-c_back, ShareAirline, Airline, AllianceIndex, false)

MatchAgain:
	if mixAirline { //这里处理混航司
		for _, rout_fore := range fjson_twoleg.Route {
			if rout_fore.FI[0].Legs[0].AD == rout_fore.FI[0].Legs[1].AD {
				continue
			}

			flightinfo_output.Route = append(flightinfo_output.Route,
				Copy2FlightInfo(SegmentCode, rout_fore.DR, rout_fore))

			repeatRoutine[rout_fore.R+rout_fore.RFN] = struct{}{}
		}
	}

	for _, rout_fore := range fjson_fore.Route {

		successMatch := false
		deviationFlow := false

		for _, rout_back := range fjson_back.Route {

			if rout_fore.FI[0].Legs[0].AS != rout_back.FI[0].Legs[0].DS ||
				!mixAirline && rout_fore.FI[0].Legs[0].AD != rout_back.FI[0].Legs[0].AD ||
				mixAirline && rout_fore.FI[0].Legs[0].AD == rout_back.FI[0].Legs[0].AD {
				continue
			}

			if Airline != "" && !(rout_fore.FI[0].Legs[0].AD == Airline || rout_back.FI[0].Legs[0].AD == Airline) ||
				RepeatRoutineCheck(rout_fore.R+rout_back.R[3:], rout_fore.RFN+rout_back.RFN) {
				continue
			}

			if rout_fore.FI[0].Legs[0].ASD > rout_back.FI[0].Legs[0].DSD ||
				rout_fore.FI[0].Legs[0].ASD == rout_back.FI[0].Legs[0].DSD &&
					rout_fore.FI[0].Legs[0].AST > rout_back.FI[0].Legs[0].DST ||
				//转机时间已经超过1440,但因为多Agency存在,所以不可以break
				rout_fore.FI[0].Legs[0].ASD < rout_back.FI[0].Legs[0].DSD &&
					rout_fore.FI[0].Legs[0].AST < rout_back.FI[0].Legs[0].DST {
				continue //时间接驳不上的
			}

			subRate := (rout_fore.TM + rout_back.TM) * 100 / TM
			if DeviationRate > 100 && subRate > DeviationRate { //过滤绕航率
				deviationFlow = true
				break //continue 这里是可以直接跳过的,因为后续只有一个Leg
			}

			CDs, CMs, _, CanConnect := CanConnectTime(rout_fore.FI[0].Legs[0], rout_back.FI[0].Legs[0])
			if !CanConnect || CMs > ConnMinutes {
				continue
			}

			flightinfo_output.Route = append(flightinfo_output.Route,
				Copy2FlightInfo_V3(SegmentCode, subRate, CDs, CMs, rout_fore, rout_back))

			repeatRoutine[rout_fore.R+rout_back.R[3:]+rout_fore.RFN+rout_back.RFN] = struct{}{}

			successMatch = true
		}

		//记录未处理的前续航班
		if !secondDo && !successMatch && !deviationFlow {
			if rout_fore.FI[0].Legs[0].ASD < TravelDate {
				mustGet[rout_fore.FI[0].Legs[0].AS] = beforedate
				nextFore.Route = append(nextFore.Route, rout_fore)
			} else {
				if 1440-cacheflight.F_Time2Int(rout_fore.FI[0].Legs[0].AST) < ConnMinutes {
					mustGet[rout_fore.FI[0].Legs[0].AS] = afterdate
					nextFore.Route = append(nextFore.Route, rout_fore)
				}
			}
		}

	}

	//后续管理
	if !secondDo {
		the1_fjson_fore = fjson_fore
		the1_fjson_back = fjson_back
	}

	//获取更远日期的航班信息
	if !secondDo && len(mustGet) > 0 {

		secondDo = true
		beforeStation := make([]string, 0, 10)
		afterStation := make([]string, 0, 10)

		for station, traveldate := range mustGet {
			if traveldate == beforedate {
				beforeStation = append(beforeStation, station)
			} else {
				afterStation = append(afterStation, station)
			}
		}

		chan_before := make(chan *cacheflight.FlightJSON, 1)
		if len(beforeStation) == 0 {
			chan_before <- &cacheflight.FlightJSON{}
		} else {
			go QueryLegInfo_V3(&cacheflight.RoutineService{
				Deal:           "QueryBackLegsDays",
				DepartStation:  "***", //DepartStation,
				ConnectStation: beforeStation,
				ArriveStation:  ArriveStation,
				TravelDate:     beforedate,
				Legs:           1,
				Days:           1},
				cachestation.PetchIP[ArriveStation], chan_before)
		}

		chan_after := make(chan *cacheflight.FlightJSON, 1)
		if len(afterStation) == 0 {
			chan_after <- &cacheflight.FlightJSON{}
		} else {
			go QueryLegInfo_V3(&cacheflight.RoutineService{
				Deal:           "QueryBackLegsDays",
				DepartStation:  "***", //DepartStation,
				ConnectStation: afterStation,
				ArriveStation:  ArriveStation,
				TravelDate:     afterdate,
				Legs:           1,
				Days:           1},
				cachestation.PetchIP[ArriveStation], chan_after)
		}

		fjson_fore = nextFore                                                               //nextFore是未匹配到的直飞航班
		fjson_back = ReduceRout(<-chan_before, ShareAirline, Airline, AllianceIndex, false) //这里的before是前一天(的第2段航班)
		fjson_back.Route = append(fjson_back.Route, ReduceRout(<-chan_after, ShareAirline, Airline, AllianceIndex, false).Route...)

		goto MatchAgain
	}

	//当达不到接驳数时,进行混航司
	if len(flightinfo_output.Route) <= MixCount && !mixAirline {

		secondDo = true
		mixAirline = true
		fjson_fore = the1_fjson_fore
		fjson_back.Route = append(the1_fjson_back.Route, fjson_back.Route...)

		goto MatchAgain
	}

}

//获取一次转机的航班处理(动态计算接驳地,不过滤绕航率的)  这里是计算一次转机
func MatchingTwoLeg_V2(
	SegmentCode string,
	DepartStation string,
	ArriveStation string,
	TravelDate string,
	flightinfo_in FlightInfo_In,
	GetOneLeg bool,
	flightinfo_out chan *FlightInfo_Output) {

	flightinfo_output := &FlightInfo_Output{Route: make([]*FlightInfo, 0, 700)}

	defer func() {
		flightinfo_out <- flightinfo_output
	}()

	TM := cacheflight.DestGetDistance(DepartStation, ArriveStation) //两地之间的距离

	if TM == 0 {
		return
	}

	DeviationRate, _ := strconv.Atoi(flightinfo_in.DeviationRate) //绕航率这里只用在计算获取转机地的最大距离
	ConnMinutes, _ := strconv.Atoi(flightinfo_in.ConnMinutes) // 中转总时间，默认300分钟
	Airline := flightinfo_in.Airline
	//lenairline := len(Airline)
	Alliance := flightinfo_in.Alliance
	AllianceIndex := mysqlop.AllianceIndex(Alliance)  //关于航司联盟的索引
	ShareAirline := flightinfo_in.ShareAirline  //共享代码
	MixCount, _ := strconv.Atoi(flightinfo_in.MixCount) //这是混舱
	var ConnStaMap map[string]struct{}

	c_twoleg := make(chan *cacheflight.FlightJSON, 1)

	//Legs 传2 代表1次中转；；； 而Days==1代表只获取出发日期那天的数据，不去获取其他天的数据。这里最终是要去cacheflight里面找缓存数据

	//我们这里暂时理解程最后的数据已经都在c_twoleg里面了
	go QueryLegInfo_V3(&cacheflight.RoutineService{
		Deal:           "QueryForeLegsDays",
		DepartStation:  DepartStation,
		ConnectStation: []string{},
		ArriveStation:  ArriveStation,
		TravelDate:     TravelDate,
		Legs:           2,
		Days:           1},
		cachestation.PetchIP[DepartStation], c_twoleg)

	var connectStation []string
	var ok bool
	if len(flightinfo_in.Legs[0].ConnectStation) == 3 {
		if connectStation, ok = cachestation.CityCounty[flightinfo_in.Legs[0].ConnectStation]; !ok {
			if _, ok = cachestation.County[flightinfo_in.Legs[0].ConnectStation]; ok {
				connectStation = []string{flightinfo_in.Legs[0].ConnectStation}
			} else {
				return
			}
		}
		ConnStaMap = make(map[string]struct{}, len(connectStation))
		for _, cs := range connectStation {
			ConnStaMap[cs] = struct{}{}
		}
	} else {
		ConnectStationCache.mutex.RLock()
		connectStation, ok = ConnectStationCache.ConnectStation[DepartStation+ArriveStation+"5"]
		ConnectStationCache.mutex.RUnlock()
		if !ok {
			//传入出发地，目的地，最大航段数，绕航率。最终会得到一个中转地机场数组
			connectStation, _ = IntelligentConnect_V5(DepartStation, ArriveStation, 2, DeviationRate)
			ConnectStationCache.mutex.Lock()
			ConnectStationCache.ConnectStation[DepartStation+ArriveStation+"5"] = connectStation
			ConnectStationCache.mutex.Unlock()
		}
	}

	tmparrive := "***"
	if GetOneLeg {
		tmparrive = ArriveStation
	}
	c_fore := make(chan *cacheflight.FlightJSON, 1)
	go QueryLegInfo_V3(&cacheflight.RoutineService{
		Deal:           "QueryForeLegsDays",
		DepartStation:  DepartStation,
		ConnectStation: connectStation,
		ArriveStation:  tmparrive, //ArriveStation,
		TravelDate:     TravelDate,
		Legs:           1,
		Days:           1},
		cachestation.PetchIP[DepartStation], c_fore)

	c_back := make(chan *cacheflight.FlightJSON, 1)

	go QueryLegInfo_V3(&cacheflight.RoutineService{
		Deal:           "QueryBackLegsDays",
		DepartStation:  "***", //DepartStation,
		ConnectStation: connectStation,
		ArriveStation:  ArriveStation,
		TravelDate:     TravelDate,
		Legs:           1,
		Days:           2},
		cachestation.PetchIP[ArriveStation], c_back)

	secondDo := false   //不同日期的匹配
	mixAirline := false //不同航司的接驳 (1)相同航司/日期接驳;(2)相同航司不同日期接驳;(3)不同航司相同日期接驳

	date, _ := time.Parse("2006-01-02", TravelDate)
	beforedate := date.AddDate(0, 0, -1).Format("2006-01-02") //获取前一天数据
	afterdate := date.AddDate(0, 0, 2).Format("2006-01-02")   //获取后天数据

	//以下是复杂业务的变量
	mustGet := make(map[string]string, 50) //直飞未匹配到航班的 string(1)=Routine+FlightNumber,string(2)=TravelDate
	nextFore := &cacheflight.FlightJSON{
		Route: make([]*cacheflight.RoutineInfoStruct, 0, 100)} //登记直飞接驳不到的航班
	var the1_fjson_fore *cacheflight.FlightJSON //用于保存TravelDate数据,以备不同航司接驳使用
	var the1_fjson_back *cacheflight.FlightJSON //用于保存TravelDate数据,以备不同航司接驳使用

	repeatRoutine := make(map[string]struct{}, 150)

	RepeatRoutineCheck := func(routine, flightNumber string) bool {
		_, ok := repeatRoutine[routine+flightNumber]
		return ok
	}

	fjson_twoleg := ReduceRout(<-c_twoleg, ShareAirline, Airline, AllianceIndex, true)
	//直接中转存在的数据
	for _, rout_fore := range fjson_twoleg.Route { //这里是非混航司数据
		if rout_fore.FI[0].Legs[0].AD != rout_fore.FI[0].Legs[1].AD {
			continue
		}

		if ConnStaMap != nil {
			if _, ok := ConnStaMap[rout_fore.FI[0].Legs[0].AS]; !ok {
				continue
			}
		}

		flightinfo_output.Route = append(flightinfo_output.Route,
			Copy2FlightInfo(SegmentCode, rout_fore.DR, rout_fore))

		repeatRoutine[rout_fore.R+rout_fore.RFN] = struct{}{}
	}

	fjson_fore := ReduceRout(<-c_fore, ShareAirline, Airline, AllianceIndex, false)
	fjson_back := ReduceRout(<-c_back, ShareAirline, Airline, AllianceIndex, false)

MatchAgain:
	if mixAirline { //这里是混航司数据
		for _, rout_fore := range fjson_twoleg.Route {
			if rout_fore.FI[0].Legs[0].AD == rout_fore.FI[0].Legs[1].AD {
				continue
			}

			if ConnStaMap != nil {
				if _, ok := ConnStaMap[rout_fore.FI[0].Legs[0].AS]; !ok {
					continue
				}
			}

			flightinfo_output.Route = append(flightinfo_output.Route,
				Copy2FlightInfo(SegmentCode, rout_fore.DR, rout_fore))

			repeatRoutine[rout_fore.R+rout_fore.RFN] = struct{}{}
		}
	}

	for _, rout_fore := range fjson_fore.Route {

		if rout_fore.FI[0].Legs[0].AS == ArriveStation { //直飞数据
			if mixAirline || Airline != "" && Airline != rout_fore.FI[0].Legs[0].AD {
				continue
			}

			flightinfo_output.Route = append(flightinfo_output.Route, Copy2FlightInfo(SegmentCode, 100, rout_fore))
			continue //前续查询会返回目的地航班,但在OneLeg处理过程已经包含
		}

		successMatch := false
		deviationFlow := false

		for _, rout_back := range fjson_back.Route {

			if rout_fore.FI[0].Legs[0].AS != rout_back.FI[0].Legs[0].DS ||
				!mixAirline && rout_fore.FI[0].Legs[0].AD != rout_back.FI[0].Legs[0].AD ||
				mixAirline && rout_fore.FI[0].Legs[0].AD == rout_back.FI[0].Legs[0].AD {
				continue
			}

			//Airline这里集中处理是因为只要一个航司适合就够了
			if Airline != "" && !(rout_fore.FI[0].Legs[0].AD == Airline || rout_back.FI[0].Legs[0].AD == Airline) ||
				RepeatRoutineCheck(rout_fore.R+rout_back.R[3:], rout_fore.RFN+rout_back.RFN) {
				continue
			}

			if rout_fore.FI[0].Legs[0].ASD > rout_back.FI[0].Legs[0].DSD ||
				rout_fore.FI[0].Legs[0].ASD == rout_back.FI[0].Legs[0].DSD &&
					rout_fore.FI[0].Legs[0].AST > rout_back.FI[0].Legs[0].DST ||
				//转机时间已经超过1440,但因为多Agency存在,所以不可以break
				rout_fore.FI[0].Legs[0].ASD < rout_back.FI[0].Legs[0].DSD &&
					rout_fore.FI[0].Legs[0].AST < rout_back.FI[0].Legs[0].DST {
				continue //时间接驳不上的
			}

			subRate := (rout_fore.TM + rout_back.TM) * 100 / TM

			CDs, CMs, _, CanConnect := CanConnectTime(rout_fore.FI[0].Legs[0], rout_back.FI[0].Legs[0])
			if !CanConnect || CMs > ConnMinutes {
				continue
			}

			flightinfo_output.Route = append(flightinfo_output.Route,
				Copy2FlightInfo_V3(SegmentCode, subRate, CDs, CMs, rout_fore, rout_back))

			repeatRoutine[rout_fore.R+rout_back.R[3:]+rout_fore.RFN+rout_back.RFN] = struct{}{}

			successMatch = true
		}

		//记录未处理的前续航班
		if !secondDo && !successMatch && !deviationFlow {
			if rout_fore.FI[0].Legs[0].ASD < TravelDate {
				mustGet[rout_fore.FI[0].Legs[0].AS] = beforedate
				nextFore.Route = append(nextFore.Route, rout_fore)
			} else {
				if 1440-cacheflight.F_Time2Int(rout_fore.FI[0].Legs[0].AST) < ConnMinutes {
					mustGet[rout_fore.FI[0].Legs[0].AS] = afterdate
					nextFore.Route = append(nextFore.Route, rout_fore)
				}
			}
		}

	}

	//后续管理
	if !secondDo {
		the1_fjson_fore = fjson_fore
		the1_fjson_back = fjson_back
	}

	//获取更远日期的航班信息***这种算法会导致很多目的地再次到远程获取数据,因为以条前续匹配不到就会进行
	if !secondDo && len(mustGet) > 0 {

		secondDo = true
		beforeStation := make([]string, 0, 10)
		afterStation := make([]string, 0, 10)

		for station, traveldate := range mustGet {
			if traveldate == beforedate {
				beforeStation = append(beforeStation, station)
			} else {
				afterStation = append(afterStation, station)
			}
		}

		chan_before := make(chan *cacheflight.FlightJSON, 1)
		if len(beforeStation) == 0 {
			chan_before <- &cacheflight.FlightJSON{}
		} else {
			go QueryLegInfo_V3(&cacheflight.RoutineService{
				Deal:           "QueryBackLegsDays",
				DepartStation:  "***", //DepartStation,
				ConnectStation: beforeStation,
				ArriveStation:  ArriveStation,
				TravelDate:     beforedate,
				Legs:           1,
				Days:           1},
				cachestation.PetchIP[ArriveStation], chan_before)
		}

		chan_after := make(chan *cacheflight.FlightJSON, 1)
		if len(afterStation) == 0 {
			chan_after <- &cacheflight.FlightJSON{}
		} else {
			go QueryLegInfo_V3(&cacheflight.RoutineService{
				Deal:           "QueryBackLegsDays",
				DepartStation:  "***", //DepartStation,
				ConnectStation: afterStation,
				ArriveStation:  ArriveStation,
				TravelDate:     afterdate,
				Legs:           1,
				Days:           1},
				cachestation.PetchIP[ArriveStation], chan_after)
		}

		fjson_fore = nextFore                                                               //nextFore是未匹配到的直飞航班
		fjson_back = ReduceRout(<-chan_before, ShareAirline, Airline, AllianceIndex, false) //这里的before是前一天(的第2段航班)
		fjson_back.Route = append(fjson_back.Route, ReduceRout(<-chan_after, ShareAirline, Airline, AllianceIndex, false).Route...)

		goto MatchAgain
	}

	//当达不到接驳数时,进行混航司
	if len(flightinfo_output.Route) <= MixCount && !mixAirline {

		secondDo = true
		mixAirline = true
		fjson_fore = the1_fjson_fore
		fjson_back.Route = append(the1_fjson_back.Route, fjson_back.Route...)

		goto MatchAgain
	}

}



//获取二此转机的航班处理(混航司模式有机会是长时间的计算)
func MatchingThreeLeg_V1(
	SegmengCode string,
	DepartStation string,
	ArriveStation string,
	TravelDate string,
	flightinfo_in FlightInfo_In,
	LegMode string, //航段相加模式 "1+2" Or "2+1"..如果指定中转地，那么可能计算到2+1.其实可以先根据热度判断模式。
	flightinfo_out chan *FlightInfo_Output) {

	flightinfo_output := &FlightInfo_Output{Route: make([]*FlightInfo, 0, 500)}
	flightinfo_fore := &FlightInfo_Output{}
	flightinfo_back := &FlightInfo_Output{Route: make([]*FlightInfo, 0, 500)}
	DeviationRate, _ := strconv.Atoi(flightinfo_in.DeviationRate)
	ConnMinutes, _ := strconv.Atoi(flightinfo_in.ConnMinutes)
	Airline := flightinfo_in.Airline
	//lenairline := len(Airline)
	Alliance := flightinfo_in.Alliance
	AllianceIndex := mysqlop.AllianceIndex(Alliance)
	ShareAirline := flightinfo_in.ShareAirline
	TM := cacheflight.DestGetDistance(DepartStation, ArriveStation)
	MixCount, _ := strconv.Atoi(flightinfo_in.MixCount)
	mixAirline := false

	defer func() {
		flightinfo_out <- flightinfo_output
	}()

	if TM == 0 {
		return
	}

	//第一部分获取直飞数据
	var connectStation []string
	var ok bool

	//如果第一段中转地的长度为3，则查对应的中转地。如果查得到的话，则将该中转地所有的机场存到connectStation 这个数组里面去
	if len(flightinfo_in.Legs[0].ConnectStation) == 3 {
		if connectStation, ok = cachestation.CityCounty[flightinfo_in.Legs[0].ConnectStation]; !ok {
			if _, ok = cachestation.County[flightinfo_in.Legs[0].ConnectStation]; ok {
				connectStation = []string{flightinfo_in.Legs[0].ConnectStation}
			} else {
				return
			}
		}
	} else {


		//在缓存中查询。利用出发地目的地+4  例如CANBKK4 当成key来计算
		ConnectStationCache.mutex.RLock()
		connectStation, ok = ConnectStationCache.ConnectStation[DepartStation+ArriveStation+"4"]
		ConnectStationCache.mutex.RUnlock()
		if !ok {
			var lenairport int
			connectStation, lenairport = IntelligentConnect_V4(DepartStation, ArriveStation, 3, DeviationRate)
			connectStation2 := IntelligentConnect_V3(DepartStation)

			for _, station := range connectStation2 {
				i := 0
				for ; i < lenairport; i++ {
					if connectStation[i] == station {
						break
					}
				}

				if i == lenairport {
					connectStation = append(connectStation, station)
				}
			}

			ConnectStationCache.mutex.Lock()
			ConnectStationCache.ConnectStation[DepartStation+ArriveStation+"4"] = connectStation
			ConnectStationCache.mutex.Unlock()
		}
	}

	if len(connectStation) == 0 {
		flightinfo_out <- flightinfo_output
		return
	}

	c_fore := make(chan *cacheflight.FlightJSON, 1)
	if LegMode == "1+2" {
		QueryLegInfo_V3(&cacheflight.RoutineService{
			Deal:           "QueryForeLegsDays",
			DepartStation:  DepartStation,
			ConnectStation: connectStation,
			ArriveStation:  "***", //ArriveStation,
			TravelDate:     TravelDate,
			Legs:           1,
			Days:           1},
			cachestation.PetchIP[DepartStation], c_fore)
	} else { //2+1的业务是指定中转查询,那么中转地是固定的
		QueryLegInfo_V3(&cacheflight.RoutineService{
			Deal:           "QueryBackLegsDays",
			DepartStation:  "***",
			ConnectStation: connectStation,
			ArriveStation:  ArriveStation,
			TravelDate:     TravelDate,
			Legs:           1,
			Days:           2}, //2+1模式中,接驳时可能使用到第2天的数据
			cachestation.PetchIP[ArriveStation], c_fore)
	}
	fjson_fore := ReduceRout(<-c_fore, ShareAirline, Airline, AllianceIndex, false)

	//第二部分,归类fjson_fore,归类内容,(1)到达的目的地,(2)到达目的地的最后日期
	flightinfo_fore.Route = make([]*FlightInfo, 0, len(fjson_fore.Route))
	destdate := make(map[string]string)
	for _, rout := range fjson_fore.Route {

		flightinfo_fore.Route = append(flightinfo_fore.Route,
			Copy2FlightInfo(SegmengCode, rout.DR, rout))

		if LegMode == "1+2" {
			//这里主要是为了减少处理的数据量,所以选择最晚时间
			if date, ok := destdate[rout.FI[0].Legs[0].AS]; !ok || date < rout.FI[0].Legs[0].ASD {
				destdate[rout.FI[0].Legs[0].AS] = rout.FI[0].Legs[0].ASD
			}
		} else {
			//2+1模式日期取最早的.
			destdate[rout.FI[0].Legs[0].DS] = TravelDate
		}
	}

	//第三部分,分配处理
	flightinfo_chan := make([]chan *FlightInfo_Output, len(destdate))
	cycle := 0

	for dest, date := range destdate {
		flightinfo_chan[cycle] = make(chan *FlightInfo_Output, 1)

		//其实以下只获取1天的数据的操作,部分数据是难以得到匹配的,幸好有2+1作为补充
		if LegMode == "1+2" {
			go MatchingTwoLeg_V1(SegmengCode, dest, ArriveStation, date, flightinfo_in, flightinfo_chan[cycle])
		} else {
			go MatchingTwoLeg_V1(SegmengCode, DepartStation, dest, date, flightinfo_in, flightinfo_chan[cycle])
		}

		cycle++
	}

	//这里返回的数据,在指定航司的情况下,已经是包含航司了
	for cycle := 0; cycle < len(destdate); cycle++ {
		flightinfo_back.Route = append(flightinfo_back.Route, (<-flightinfo_chan[cycle]).Route...)
	}

	//如果LegMode="2+1",必须把匹配的变量转换过来,因为他们获取时是反过来的。
	if LegMode == "2+1" {
		flightinfo_fore, flightinfo_back = flightinfo_back, flightinfo_fore
	}

	var Last int //用于匹配是表达rout_fore.Legs的长度
	if LegMode == "2+1" {
		Last = 1
	}

	//第四部分,合并处理
MatchAgain:
	for _, rout_fore := range flightinfo_fore.Route { //fjson_fore.Route

		if rout_fore.Legs[0].AS == ArriveStation { //LegMode == "2+1" 时产生这种结果
			continue
		}

		if mixAirline && len(flightinfo_output.Route) > 200 {
			break
		}

		for _, rout_back := range flightinfo_back.Route {

			if rout_fore.Legs[Last].AS != rout_back.Legs[0].DS ||
				!mixAirline && //混舱这里是有改进空间的
					(rout_fore.Legs[0].AD != rout_back.Legs[0].AD &&
						(LegMode == "1+2" && rout_back.Legs[0].AD != rout_back.Legs[1].AD ||
							LegMode == "2+1" && rout_fore.Legs[0].AD != rout_fore.Legs[1].AD)) ||
				mixAirline &&
					(rout_fore.Legs[0].AD == rout_back.Legs[0].AD ||
						(LegMode == "1+2" && rout_back.Legs[0].AD == rout_back.Legs[1].AD ||
							LegMode == "2+1" && rout_fore.Legs[0].AD == rout_fore.Legs[1].AD)) {
				continue
			}

			if DepartStation == rout_back.Legs[0].AS { //LegMode == "1+2" 时产生这种结果
				continue //CAN-**-***-**-CAN-**-***
			}

			if rout_fore.Legs[Last].ASD > rout_back.Legs[0].DSD ||
				rout_fore.Legs[Last].ASD == rout_back.Legs[0].DSD &&
					rout_fore.Legs[Last].AST > rout_back.Legs[0].DST ||
				//转机时间已经超过1440,但因为多Agency存在,所以不可以break
				rout_fore.Legs[Last].ASD < rout_back.Legs[0].DSD &&
					rout_fore.Legs[Last].AST < rout_back.Legs[0].DST {
				continue //时间接驳不上的
			}

			//if Airline != "" && Airline != rout_fore.Legs[0].AD && Airline != rout_back.Legs[0].AD {
			//	if LegMode == "1+2" && Airline != rout_back.Legs[1].AD ||
			//		LegMode == "2+1" && Airline != rout_fore.Legs[1].AD {
			//		continue
			//	}
			//}TwoLeg部分已经包含有效航司了,OneLeg部分包不包含都没关系

			CDs, CMs, _, CanConnect := CanConnectTime(rout_fore.Legs[Last], rout_back.Legs[0])
			if !CanConnect || CMs > ConnMinutes {
				continue
			}

			subRate := (rout_fore.TotleMile + rout_back.TotleMile) * 100 / TM

			flightinfo_output.Route = append(flightinfo_output.Route,
				Copy2FlightInfo_V4(SegmengCode, subRate, CDs, CMs, rout_fore, rout_back))

		}
	}

	if !mixAirline &&
		len(flightinfo_output.Route) < MixCount {
		mixAirline = true
		goto MatchAgain
	}
}



//某航段数的输出（这里可能是仅输出直飞？一次中转？二次中国转）
func MatchingFlightMain_V2(flightinfo_in FlightInfo_In) (
	flightinfo_out *FlightInfo_Output) {

	var (
		DepartStation   []string
		ArriveStation   []string
		ok              bool
		legLegs         int
		flightinfo_chan [][]chan *FlightInfo_Output //用于机场对机场的接驳航班输出
	)

	legLegs = len(flightinfo_in.Legs)

	flightinfo_out = &FlightInfo_Output{Route: make([]*FlightInfo, 0, 500)}


	flightinfo_chan = make([][]chan *FlightInfo_Output, legLegs)

	//遍历传递进来的Legs，，，，里面存了出发，到达  出发日期
	for SegmentCode, info_city := range flightinfo_in.Legs {

		 // SegmentCode 代表的是第几段，其实就是index，索引
		 //info_city 代表的是每一段里面的详情
		//出发地城市对应到机场
		if DepartStation, ok = cachestation.CityCounty[info_city.DepartStation]; !ok {
			if _, ok = cachestation.County[info_city.DepartStation]; ok {
				DepartStation = []string{info_city.DepartStation}
			} else {
				//DepartStation = []string{}
				return
			}
		}
		//目的地城市对应到机场
		if ArriveStation, ok = cachestation.CityCounty[info_city.ArriveStation]; !ok {
			if _, ok = cachestation.County[info_city.ArriveStation]; ok {
				ArriveStation = []string{info_city.ArriveStation}
			} else {
				//ArriveStation = []string{}
				return
			}
		}

		//从上面拿到的DepartStation，ArriveStation 其实是机场数组。里面存了出发地的机场数组，以及目的地的机场数组。类似北京有两个机场，上海有两个机场。这样组合起来的话，就有4种。每一段都有出发*目的  种类型
		flightinfo_chan[SegmentCode] = make([]chan *FlightInfo_Output, len(DepartStation)*len(ArriveStation))

		cycle := 0


		//遍历出发地每个机场
		for _, depart := range DepartStation {
			//遍历目的地每个机场
			for _, arrive := range ArriveStation {



				flightinfo_chan[SegmentCode][cycle] = make(chan *FlightInfo_Output, 1)

				if flightinfo_in.NoConnect == "1" {
					//1代表直飞数据。。后面flightinfo_chan这里是不是要赋值？？
					go MatchingOneLeg_V1(strconv.Itoa(SegmentCode+1), depart, arrive, info_city.TravelDate, flightinfo_in.ShareAirline, flightinfo_in.Airline, flightinfo_in.Alliance, flightinfo_chan[SegmentCode][cycle])
				} else if flightinfo_in.NoConnect == "2" {
					//2 代表一次中转数据
					go MatchingTwoLeg_V2(strconv.Itoa(SegmentCode+1), depart, arrive, info_city.TravelDate, flightinfo_in, false, flightinfo_chan[SegmentCode][cycle])
				} else if flightinfo_in.NoConnect == "3" {
					//3代表二次中转数据
					go MatchingThreeLeg_V1(strconv.Itoa(SegmentCode+1), depart, arrive, info_city.TravelDate, flightinfo_in, "1+2", flightinfo_chan[SegmentCode][cycle])
				} else {
					flightinfo_chan[SegmentCode][cycle] <- &FlightInfo_Output{}
				}

				cycle++
			}
		}
	} //这里是发送完毕的地方



	for SegmentCode := range flightinfo_in.Legs {

		for cycle := range flightinfo_chan[SegmentCode] {

			//上面是一段段存进去。这里是一段段重新拉出来
			flightinfo_out.Route = append(flightinfo_out.Route,
				(<-flightinfo_chan[SegmentCode][cycle]).Route...)
		}
	}

	return
}



//比MatchingFlightMain_V2多输出控制，所有航段的输出。（包括直飞，中转1次，2次）
//#TODO 这里是输出所有的数据，包括直飞，中转1次，中转2次等
func MatchingFlightMain_V3(flightinfo_in FlightInfo_In) (
	flightinfo_out *FlightInfo_Output) {


	var (
		DepartStation     []string
		ArriveStation     []string
		ok                bool
		legLegs           int
		flightinfo_out_V2 *FlightInfo_Output
		flightinfo_chan   [][]chan *FlightInfo_Output //用于机场对机场的接驳航班输出
		RoutineRecord     map[string]int
		DoLeg             int = 1 //(1)=OneLeg (2)=TwoLeg (3)=ThreeLeg
		GetLeg            int = 1 //(1)="11" (2)="21" (3)="22" (4)="31" (5)="32" (6)="33" doing
	)

	legLegs = len(flightinfo_in.Legs)  //查询几段。
	RoutineCount, _ := strconv.Atoi(flightinfo_in.RoutineCount)
	flightinfo_out = &FlightInfo_Output{Route: make([]*FlightInfo, 0, 500)}
	flightinfo_chan = make([][]chan *FlightInfo_Output, legLegs)
	RoutineRecord = make(map[string]int, RoutineCount+10)

MatchAgain:
	flightinfo_out_V2 = &FlightInfo_Output{Route: make([]*FlightInfo, 0, 500)}

	//遍历传进来的要查询的行程航程
	for SegmentCode, info_city := range flightinfo_in.Legs {

		//这里的SegmentCode 其实代表的是索引，也就是我们要查询到第几段。例如【CAN BKK】 [NAS BJS] [SHA SWA] 其实这就是指的是哪一段

		//出发地城市对应到机场（查看缓存里面是否有出发地这个记录。）
		if DepartStation, ok = cachestation.CityCounty[info_city.DepartStation]; !ok {
			//传入机场三字代码，后面是该机场的一些信息
			if _, ok = cachestation.County[info_city.DepartStation]; ok {

				DepartStation = []string{info_city.DepartStation}
			} else {
				//DepartStation = []string{}
				return
			}
		}
		//目的地城市对应到机场（同上）
		if ArriveStation, ok = cachestation.CityCounty[info_city.ArriveStation]; !ok {
			if _, ok = cachestation.County[info_city.ArriveStation]; ok {
				ArriveStation = []string{info_city.ArriveStation}
			} else {
				//ArriveStation = []string{}
				return
			}
		}

		//DepartStation   ArriveStation  其实代表出发地数组，目的地的数组
		//后面的len(DepartStation)*len(ArriveStation) 是否为北京有2个机场，上面有2个机场，因此2*2；其他的同理
		flightinfo_chan[SegmentCode] = make([]chan *FlightInfo_Output, len(DepartStation)*len(ArriveStation))
		cycle := 0

		for _, depart := range DepartStation {
			for _, arrive := range ArriveStation {

				//这里取得是第SegmentCode段，第cycle个选择
				flightinfo_chan[SegmentCode][cycle] = make(chan *FlightInfo_Output, 1)

				if DoLeg == 1 { //制作1个航段,但1个航段可以与2个航段同时制作
					if RoutineCount <= 1 {
						go MatchingOneLeg_V1(strconv.Itoa(SegmentCode+1), depart, arrive, info_city.TravelDate, flightinfo_in.ShareAirline, flightinfo_in.Airline, flightinfo_in.Alliance, flightinfo_chan[SegmentCode][cycle])
					} else {
						go MatchingTwoLeg_V2(strconv.Itoa(SegmentCode+1), depart, arrive, info_city.TravelDate, flightinfo_in, true, flightinfo_chan[SegmentCode][cycle])
					}
				} else if DoLeg == 2 { //制作2个航段
					go MatchingTwoLeg_V2(strconv.Itoa(SegmentCode+1), depart, arrive, info_city.TravelDate, flightinfo_in, false, flightinfo_chan[SegmentCode][cycle])
				} else if DoLeg == 3 { //制作3个航段
					go MatchingThreeLeg_V1(strconv.Itoa(SegmentCode+1), depart, arrive, info_city.TravelDate, flightinfo_in, "1+2", flightinfo_chan[SegmentCode][cycle])
				} else {
					flightinfo_chan[SegmentCode][cycle] <- &FlightInfo_Output{}
				}

				cycle++
			}
		}
	} //这里是发送完毕的地方


	//数据处理完毕后，通过对比数据输入
	for SegmentCode := range flightinfo_in.Legs {
		for cycle := range flightinfo_chan[SegmentCode] {

			flightinfo_out_V2.Route = append(flightinfo_out_V2.Route,
				(<-flightinfo_chan[SegmentCode][cycle]).Route...)
		}
	}



	//这里是检测数量的地方
GetFlightAgain:
	for _, rout := range flightinfo_out_V2.Route {
		if GetLeg == 1 && rout.TripType == "11" ||
			GetLeg == 2 && rout.TripType == "21" ||
			GetLeg == 3 && rout.TripType == "22" ||
			GetLeg == 4 && rout.TripType == "31" ||
			GetLeg == 5 && rout.TripType == "32" ||
			GetLeg == 6 && rout.TripType == "33" {

			RoutineRecord[ShortRoutine_V2(rout.Routine)]++
			flightinfo_out.Route = append(flightinfo_out.Route, rout)
		}
	}

	if len(RoutineRecord) >= RoutineCount {
		return
	}

	GetLeg++

	if DoLeg == 1 {
		if RoutineCount > 1 {
			if GetLeg < 4 {
				goto GetFlightAgain
			}
		}

		if RoutineCount <= 1 {
			DoLeg = 2 //如果要是获取1个路线,但路线找不到,其实是需要再次获取1次中转
		} else {
			DoLeg = 3 //前面是同时获取1,2航段的,直接进入获取3个航段
		}

	} else if DoLeg == 2 {
		if GetLeg < 4 {
			goto GetFlightAgain
		}

		DoLeg = 3

	} else {
		if GetLeg < 7 {
			goto GetFlightAgain
		}

		DoLeg = 7
	}

	if len(RoutineRecord) < RoutineCount && DoLeg < 7 {
		goto MatchAgain
	}

	return
}




//在MatchingFlightMain_V3的基础上,增加中转地控制。。。。。。这里应该就是牛逼的地方了把
func MatchingFlightMain_V4(flightinfo_in FlightInfo_In) (
	flightinfo_out *FlightInfo_Output) {

	var (
		DepartStation     []string
		ArriveStation     []string
		ok                bool
		legLegs           int
		flightinfo_out_V2 *FlightInfo_Output
		flightinfo_chan   [][]chan *FlightInfo_Output //用于机场对机场的接驳航班输出
		RoutineRecord     map[string]int
		DoLeg             = 2 //(1)=OneLeg (2)=TwoLeg (3)=ThreeLeg
		GetLeg            = 2 //(1)="11" (2)="21" (3)="22" (4)="31" (5)="32" (6)="33" doing
	)

	legLegs = len(flightinfo_in.Legs)
	RoutineCount, _ := strconv.Atoi(flightinfo_in.RoutineCount)
	flightinfo_out = &FlightInfo_Output{Route: make([]*FlightInfo, 0, 500)}
	flightinfo_chan = make([][]chan *FlightInfo_Output, legLegs)
	RoutineRecord = make(map[string]int, RoutineCount+10)

	if RoutineCount > 40 {
		RoutineCount = 20
	} else if RoutineCount > 20 {
		RoutineCount = 15
	}
MatchAgain:
	flightinfo_out_V2 = &FlightInfo_Output{Route: make([]*FlightInfo, 0, 500)}
	for SegmentCode, info_city := range flightinfo_in.Legs {
		//出发地城市对应到机场
		if DepartStation, ok = cachestation.CityCounty[info_city.DepartStation]; !ok {
			if _, ok = cachestation.County[info_city.DepartStation]; ok {
				DepartStation = []string{info_city.DepartStation}
			} else {
				return
			}
		}
		//目的地城市对应到机场
		if ArriveStation, ok = cachestation.CityCounty[info_city.ArriveStation]; !ok {
			if _, ok = cachestation.County[info_city.ArriveStation]; ok {
				ArriveStation = []string{info_city.ArriveStation}
			} else {
				return
			}
		}

		if DoLeg != 3 {
			flightinfo_chan[SegmentCode] = make([]chan *FlightInfo_Output, len(DepartStation)*len(ArriveStation))
		} else {
			flightinfo_chan[SegmentCode] = make([]chan *FlightInfo_Output, len(DepartStation)*len(ArriveStation)*2)
		}
		cycle := 0

		for _, depart := range DepartStation {
			for _, arrive := range ArriveStation {
				flightinfo_chan[SegmentCode][cycle] = make(chan *FlightInfo_Output, 1)

				if DoLeg == 2 {
					go MatchingTwoLeg_V2(strconv.Itoa(SegmentCode+1), depart, arrive, info_city.TravelDate, flightinfo_in, false, flightinfo_chan[SegmentCode][cycle])
				} else if DoLeg == 3 {
					go MatchingThreeLeg_V1(strconv.Itoa(SegmentCode+1), depart, arrive, info_city.TravelDate, flightinfo_in, "1+2", flightinfo_chan[SegmentCode][cycle])
					cycle++
					flightinfo_chan[SegmentCode][cycle] = make(chan *FlightInfo_Output, 1)
					go MatchingThreeLeg_V1(strconv.Itoa(SegmentCode+1), depart, arrive, info_city.TravelDate, flightinfo_in, "2+1", flightinfo_chan[SegmentCode][cycle])
				} else {
					flightinfo_chan[SegmentCode][cycle] <- &FlightInfo_Output{}
				}

				cycle++
			}
		}
	} //这里是发送完毕的地方

	for SegmentCode := range flightinfo_in.Legs {
		for cycle := range flightinfo_chan[SegmentCode] {

			flightinfo_out_V2.Route = append(flightinfo_out_V2.Route,
				(<-flightinfo_chan[SegmentCode][cycle]).Route...)
		}
	}

	//这里是检测数量的地方
GetFlightAgain:
	for _, rout := range flightinfo_out_V2.Route {
		if GetLeg == 2 && rout.TripType == "21" ||
			GetLeg == 3 && rout.TripType == "22" ||
			GetLeg == 4 && rout.TripType == "31" ||
			GetLeg == 5 && rout.TripType == "32" ||
			GetLeg == 6 && rout.TripType == "33" {

			RoutineRecord[ShortRoutine_V2(rout.Routine)]++
			flightinfo_out.Route = append(flightinfo_out.Route, rout)
		}
	}

	if len(RoutineRecord) >= RoutineCount {
		return
	}

	GetLeg++

	if DoLeg == 2 {
		if GetLeg < 4 {
			goto GetFlightAgain
		}

		DoLeg = 3

	} else { // else if DoLeg==3
		if GetLeg < 7 {
			goto GetFlightAgain
		}

		DoLeg = 7
	}

	if len(RoutineRecord) < RoutineCount && DoLeg < 7 {
		goto MatchAgain
	}

	return
}




/******************************
可飞航线的获取(行段间的组合)
(1):结果中不使用舱位信息????
(2):结果线处理程2段航程的2016-04-15
******************************/


/**
#TODO 这里就是航班时刻航班时刻！！！非常重要非常重要
*/
func WAPI_MatchingFlight_V1(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var (
		flightinfo_in       FlightInfo_In
		flightinfo_out      *FlightInfo_Output
		flightinfo_out_json []byte
	)

	//最终结果是以flightinfo_out转出去。然后flightinfo_out_json  就是返回出去外面的json
	defer func() {
		if flightinfo_out != nil && len(flightinfo_out.Route) > 0 {
			flightinfo_out_json = errorlog.Make_JSON_GZip(flightinfo_out)
		}

		if flightinfo_out_json == nil {
			fmt.Fprint(w, bytes.NewBuffer(FlightInfoErrOut))
		} else {
			fmt.Fprint(w, bytes.NewBuffer(flightinfo_out_json))
		}
	}()

	if err := json.Unmarshal(result, &flightinfo_in); err != nil {
		errorlog.WriteErrorLog("WAPI_MatchingFlight_V1 (1): " + err.Error())
		return
	}


	//判断传入的航段长度（也就是说最多只能传入两个航班）（前端其实可以做很多段，最终只不过是将各段传入去查询）
	legLegs := len(flightinfo_in.Legs)
	if legLegs == 0 || legLegs > 2 {
		errorlog.WriteErrorLog("WAPI_MatchingFlight_V1 (2): legLegs=" + strconv.Itoa(legLegs))
		return
	}

	//如果输入的参数没填，。通过判断这些参数的长度，如果参数长度为0，证明没有填入
	if len(flightinfo_in.Airline) > 0 {
		flightinfo_in.Alliance = ""
	}
	if len(flightinfo_in.DeviationRate) == 0 {
		flightinfo_in.DeviationRate = "210"
	}
	if len(flightinfo_in.ConnMinutes) == 0 {
		flightinfo_in.ConnMinutes = "1440"
	}
	if len(flightinfo_in.ShareAirline) == 0 {
		flightinfo_in.ShareAirline = "B"
	}
	if len(flightinfo_in.NoConnect) == 0 {
		flightinfo_in.NoConnect = "0"
	}

	//触发混航司的记录数(默认20条记录
	if len(flightinfo_in.MixCount) == 0 {
		flightinfo_in.MixCount = strconv.Itoa(MixAirlineNum)
	}
	if len(flightinfo_in.RoutineCount) == 0 {
		flightinfo_in.RoutineCount = "50"
	}

	//如果第一段里面含有中转地的话，则调用MatchingFlightMain_V4（flightinfo_in）
	if len(flightinfo_in.Legs[0].ConnectStation) == 3 {
		flightinfo_out = MatchingFlightMain_V4(flightinfo_in)
	} else {

		//这里意味着第一段不会出现中转情况

		//全部输出。按1-2-3获取到RoutineCount的要求。1 直飞 2 一次转机  3 二次转机 MatchingFlightMain_V3
		if flightinfo_in.NoConnect == "0" {
			flightinfo_out = MatchingFlightMain_V3(flightinfo_in)
		} else {
			//按照传入的参数来处理。判断要直飞？一次转机？还是二次转机
			flightinfo_out = MatchingFlightMain_V2(flightinfo_in)
		}
	}
}


//QueryLegInfo_V3主要用于MatchingFlight,特点是航班要排序,但相同航班必须过滤,不然匹配时要检测相同航班更麻烦.（和刚开始做机票的时候很像很像）
func QueryLegInfo_V3(rs *cacheflight.RoutineService, serverIP string, cfjs chan *cacheflight.FlightJSON) {
	c_fjson := make(chan *cacheflight.FlightJSON, 1)

	//航段信息，ip地址，返回的航班（是不是类似回调）。。。这里的c_fjson已经获取到了应得的数据。已经去到catheflight里面调用了
	//c_fjson 里面存的就是
	QueryLegInfo(rs, serverIP, c_fjson)

	fore := <-c_fjson

	//排序  按照出发地>供应商>起飞时间>转机时长>插入时间
	sort.Sort(fore.Route)

	RoutLen := len(fore.Route)
	if RoutLen == 0 {
		cfjs <- fore
		return
	}

	mi := make([]int, 0, RoutLen)
	mf := make(map[string]struct{}, RoutLen) //string(1) =TravelDate(因为存在多天) + Routine+Number,string(2)=Agency

	for i := 0; i < RoutLen; i++ {

		//如果P是下面5个数据源之一，则代表是ok的
		if _, ok := mf[fore.Route[i].FI[0].Legs[0].DSD+fore.Route[i].R+fore.Route[i].RFN]; !ok &&
			(fore.Route[i].FI[0].Legs[0].P == "OAG" ||
				fore.Route[i].FI[0].Legs[0].P == "1E" ||
				fore.Route[i].FI[0].Legs[0].P == "1G" ||
				fore.Route[i].FI[0].Legs[0].P == "1B" ||
				fore.Route[i].FI[0].Legs[0].P == "1M") {


			//ABS航班信息中把舱位信息去除,可以减少信息量加快网络传输.
			for _, leg := range fore.Route[i].FI[0].Legs {
				leg.CI = nil
			}

			mf[fore.Route[i].FI[0].Legs[0].DSD+fore.Route[i].R+fore.Route[i].RFN] = struct{}{} //rout.FI[0].Legs[0].P
			mi = append(mi, i)
		}
	}

	if RoutLen != len(mi) {
		nrout := make(cacheflight.FlightJSONRout, 0, len(mi))
		for _, i := range mi {
			nrout = append(nrout, fore.Route[i])
		}
		//在最后这个环节，还对nrout进行多一层处理
		fore.Route = nrout //不直接截取是因为有时间和供应商问题
	}

	cfjs <- fore
}


//通过传入航班信息  获取对应的航段和航司..，通过传入来this.Legs 这个数组的长度，代表有几段，也就是第一个数字；第二个数字代表的是有几个航司
//21 代表2个航段，1个航司。   23代表2个航段3个航司
//航班信息，获取TripType（这里传入的是指针，也就是说，修改之后会将原有的数据也一起修改）
func (this *FlightInfo) GetTripType() {
	legs := len(this.Legs)

	if legs == 1 {

		this.TripType = "11"

	} else if legs == 2 {
		//如果第一段的航司和第二段的航司是想同，则21；如果不相同，则22
		if this.Legs[0].AD == this.Legs[1].AD {
			this.TripType = "21"
		} else {
			this.TripType = "22"
		}
	} else if legs == 3 {
		//如果第一段的航司==第二段的航司  而且第一段的航司等于第三段的航司，则可以确定三个航司一定都是相同的，则确定是31
		if this.Legs[0].AD == this.Legs[1].AD &&
			this.Legs[0].AD == this.Legs[2].AD {
			this.TripType = "31"
		} else if this.Legs[0].AD == this.Legs[1].AD ||
			this.Legs[0].AD == this.Legs[2].AD ||
			this.Legs[1].AD == this.Legs[2].AD {

			//如果三个航司中，只要有其中两个是一样的，另一个是不一样的，则32

			this.TripType = "32"
		} else {

			//三个航司各不同
			this.TripType = "33"
		}
	}
}
