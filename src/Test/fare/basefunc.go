package fare

import (
	"cachestation"
	"database/sql"
	"errorlog"
	"errors"
	//"fmt"
	"mysqlop"
	"outsideapi"
	"sort"
	"strconv"
	"strings"
	"time"
	"webapi"
	"bytes"
	"sync"
)



//把GDS Fare拷贝程内存的缓存格式[Copy()-->AcrossDate()-->Across()]
func Copy(this *outsideapi.GDSFare) []*mysqlop.MainBill {
	//多航线路线反转处理
	reRoutine := func(applyRoutine string) string {
		routines := strings.Split(applyRoutine, "$")
		ret := ""

		for i, routine := range routines {
			if i == 0 {
				ret = webapi.RedoRoutineMutil(routine)
			} else {
				ret += "$" + webapi.RedoRoutineMutil(routine)
			}
		}

		return ret
	}

	//多舱位时反转
	reverseBerth := func(cabin string) string {
		cabins := []byte(cabin)
		for from, to := 0, len(cabins)-1; from < to; from, to = from+1, to-1 {
			cabins[from], cabins[to] = cabins[to], cabins[from]
		}
		return string(cabins)
	}

	//这里要处理After/Before与TravelFirstDate/TravelLastDate的关系

	price, childprice := this.AdultPrice, this.ChildPrice
	Trip := 0

	if childprice == 0 {
		childprice = price
	}

	var (
		fare        []*mysqlop.MainBill
		travelDates [][2]string
		backDates   [][2]string
		weeks       [][2]int
		rerout      string
		today       = errorlog.Today()

		outbound []string
		inbound  []string
		dobound  string

		outboundairline    []string
		inboundairline     []string
		outboundnotairline []string
		inboundnotairline  []string
	)

	for _, date := range strings.Split(this.NotTravel, " ") {
		switch date {
		case "OUTBOUND", "INBOUND":
			dobound = date
		default:
			switch dobound {
			case "OUTBOUND":
				outbound = append(outbound, date)
			case "INBOUND":
				inbound = append(inbound, date)
			default:
				outbound = append(outbound, date)
			}
		}
	}

	//TravelDate
	dobound = ""
	if len(outbound) > 0 {
		dobound = strings.Join(outbound, " ")
	}
	travelDates = AcrossDate([][2]string{{this.TravelFirstDate, this.TravelLastDate}}, this.TravelDate, dobound)
	//BackDate

	dobound = ""
	if len(inbound) > 0 {
		dobound = strings.Join(inbound, " ")
	}
	if this.TripType == "RT" {
		backDates = AcrossDate([][2]string{{this.TravelFirstDate, this.TravelLastDate}}, this.BackDate, dobound)
	}
	//WeekLimit
	weeks = AcrossWeek(this.WeekLimit)

	dobound = ""
	for _, airnum := range strings.Split(this.ApplyAir, " ") {
		switch airnum {
		case "OUTBOUND", "INBOUND":
			dobound = airnum
		default:
			switch dobound {
			case "OUTBOUND":
				outboundairline = append(outboundairline, airnum)
			case "INBOUND":
				inboundairline = append(inboundairline, airnum)
			default:
				outboundairline = append(outboundairline, airnum)
			}
		}
	}

	dobound = ""
	for _, airnum := range strings.Split(this.NotFitApplyAir, " ") {
		switch airnum {
		case "OUTBOUND", "INBOUND":
			dobound = airnum
		default:
			switch dobound {
			case "OUTBOUND":
				outboundnotairline = append(outboundnotairline, airnum)
			case "INBOUND":
				inboundnotairline = append(inboundnotairline, airnum)
			default:
				outboundnotairline = append(outboundnotairline, airnum)
			}
		}
	}

	if this.TripType == "RT" {
		//price = price / 2
		//childprice = childprice / 2
		Trip = 1
		rerout = reRoutine(this.ApplyRoutine)
		fare = make([]*mysqlop.MainBill, 0, len(travelDates)*len(weeks)*2)
	} else {
		fare = make([]*mysqlop.MainBill, 0, len(travelDates)*len(weeks))
	}

	if this.TripType == "RT" { //制作的票单不是指定第一承运人第二承运人是因为,Routine是GDS指定的.
		for _, date := range travelDates {
			for _, bdate := range backDates {
				if date[0] > bdate[1] || //旅行日期大于会程日期时不适合的
					date[1] < today || bdate[1] < today {
					continue
				}

				//这里对销售限制日期只作出一次判断
				for _, week := range weeks {
					fare = append(fare, &mysqlop.MainBill{
						ID:               getID(),
						BillID:           "FareV2",
						AirInc:           this.Airline,
						Springboard:      this.Departure,
						Destination:      this.Arrival,
						FareBase:         this.FareBase,
						Berth:            this.BookingClass,
						BillBerth:        this.Cabin,
						AdultsPrice:      price,
						ChildrenPrice:    childprice,
						Trip:             Trip,
						GoorBack:         0,
						ApplyAir:         strings.Join(outboundairline, " "),
						NotFitApplyAir:   strings.Join(outboundnotairline, " "),
						MinStay:          this.MinStay,
						MaxStay:          this.MaxStay,
						TravelFirstDate:  date[0],
						TravelLastDate:   date[1],
						ReserveFirstDate: this.FirstSalesDate,
						ReserveLastDate:  this.LastSalesDate,
						WeekFirst:        week[0],
						WeekLast:         week[1],
						ApplyHumen:       "A",
						OutBill1:         this.AdvpResepvations, //这里是因为电商平台需要
						OutBill2:         this.AdvpResepvations, //这里必须保证空为365,360是12MONTH
						Agency:           this.GDS,
						PCC:              this.Agency,
						PriceOrder:       this.PriceOrder,
						Routine:          this.ApplyRoutine,                 //ApplyRoutine:     this.ApplyRoutine,
						Remark:           this.Remark,                       //Remark相同可以混舱
						Mark1:            strings.Join(inboundairline, " "), //把回程的信息写这里,方便推送到电商平台
						Mark2:            strings.Join(inboundnotairline, " "),
						Provider:         bdate[0],
						BillAttribute:    bdate[1], //Mark1,Mark2,Provider,BillAttribute只用于数据同步
						MixBerth:         1,
						NumberOfPeople:   this.NumberOfPeople,
						OperateDateTime:  this.InsertDate,
						CommandID:        this.CommandID})

					fare = append(fare, &mysqlop.MainBill{
						ID:               getID(),
						BillID:           "FareV2",
						AirInc:           this.Airline,
						Springboard:      this.Arrival,
						Destination:      this.Departure,
						FareBase:         this.FareBase,
						Berth:            reverseBerth(this.BookingClass),
						BillBerth:        this.Cabin,
						AdultsPrice:      price,
						ChildrenPrice:    childprice,
						Trip:             Trip,
						GoorBack:         1,
						ApplyAir:         strings.Join(inboundairline, " "),
						NotFitApplyAir:   strings.Join(inboundnotairline, " "),
						MinStay:          this.MinStay,
						MaxStay:          this.MaxStay,
						TravelFirstDate:  bdate[0],
						TravelLastDate:   bdate[1],
						ReserveFirstDate: this.FirstSalesDate,
						ReserveLastDate:  this.LastSalesDate,
						WeekFirst:        week[0],
						WeekLast:         week[1],
						ApplyHumen:       "A",
						OutBill1:         this.AdvpResepvations,
						OutBill2:         this.AdvpResepvations,
						Agency:           this.GDS,
						PCC:              this.Agency,
						PriceOrder:       this.PriceOrder,
						Routine:          rerout, //ApplyRoutine:     rerout,
						Remark:           this.Remark,
						MixBerth:         1,
						NumberOfPeople:   this.NumberOfPeople,
						OperateDateTime:  this.InsertDate,
						CommandID:        this.CommandID})
				}
			}
		}
	} else {
		for _, date := range travelDates {
			if date[1] < today {
				continue
			}

			//这里对销售限制日期只作出一次判断
			for _, week := range weeks {
				fare = append(fare, &mysqlop.MainBill{
					ID:               getID(),
					BillID:           "FareV2",
					AirInc:           this.Airline,
					Springboard:      this.Departure,
					Destination:      this.Arrival,
					FareBase:         this.FareBase,
					Berth:            this.BookingClass,
					BillBerth:        this.Cabin,
					AdultsPrice:      price,
					ChildrenPrice:    childprice,
					Trip:             Trip,
					GoorBack:         0,
					ApplyAir:         strings.Join(outboundairline, " "),
					NotFitApplyAir:   strings.Join(outboundnotairline, " "),
					MinStay:          this.MinStay,
					MaxStay:          this.MaxStay,
					TravelFirstDate:  date[0],
					TravelLastDate:   date[1],
					ReserveFirstDate: this.FirstSalesDate,
					ReserveLastDate:  this.LastSalesDate,
					WeekFirst:        week[0],
					WeekLast:         week[1],
					ApplyHumen:       "A",
					OutBill1:         this.AdvpResepvations,
					OutBill2:         this.AdvpResepvations, //这里必须保证空为365,360是12MONTH
					Agency:           this.GDS,
					PCC:              this.Agency,
					PriceOrder:       this.PriceOrder,
					Routine:          this.ApplyRoutine, //ApplyRoutine:     this.ApplyRoutine,
					Remark:           this.Remark,
					MixBerth:         1,
					NumberOfPeople:   this.NumberOfPeople,
					OperateDateTime:  this.InsertDate,
					CommandID:        this.CommandID})
			}
		}
	}
	return fare
}


//把TravelDate/NotTravel交叉后输出适合的日期
func AcrossDate(TravelDate [][2]string, //fare TravelFirstDate/TravelLastDate(YYYY-MM-DD)
	dates, notdates string) [][2]string { //TravelDate,NotTravel(DDMMMYY-DDMMMYY)

	splitDate := func(TD string) [][2]string {
		dates := strings.Split(TD, " ")
		rets := make([][2]string, 0, len(dates))

		for _, date := range dates {
			if len(date) == 15 {
				rets = append(rets, [2]string{errorlog.ChangeDate(date[:7]), errorlog.ChangeDate(date[8:])})
			}
		}

		return rets
	}

	if len(dates) == 0 && len(notdates) == 0 {
		return TravelDate
	} else if len(dates) != 0 && len(notdates) != 0 {
		return AcrossIn(AcrossOut(splitDate(dates), splitDate(notdates)), TravelDate)
		//return AcrossOut(splitDate(dates), splitDate(notdates))
	} else if len(dates) != 0 && len(notdates) == 0 {
		return AcrossIn(splitDate(dates), TravelDate)
	} else { //len(notdates) == 0 && len(notdates) != 0
		return AcrossOut(TravelDate, splitDate(notdates))
	}
}

//适合内的日期
func AcrossIn(traveldates, intranels [][2]string) [][2]string {

	backdates := make([][2]string, 0, len(traveldates))
	for _, traveldate := range traveldates {
		for _, intravel := range intranels { //如果这里存在相同,结果会把相同的重复.真实只一组.
			if intravel[1] < traveldate[0] || intravel[0] > traveldate[1] {
				continue
			}

			//All Operation Take traveldate In intravel
			if intravel[0] <= traveldate[0] && intravel[1] >= traveldate[1] { //In Across
				backdates = append(backdates, traveldate) //这里为traveldate会怎样?

			} else if intravel[0] <= traveldate[0] && intravel[1] < traveldate[1] { //Right Across
				backdates = append(backdates, [2]string{traveldate[0], intravel[1]})

			} else if intravel[0] >= traveldate[0] && intravel[1] >= traveldate[1] { //Left Include
				backdates = append(backdates, [2]string{intravel[0], traveldate[1]})

			} else if intravel[0] >= traveldate[0] && intravel[1] < traveldate[1] { //Out Include
				backdates = append(backdates, intravel)

			}
		}
	}

	return backdates
}

//排除掉的日期
func AcrossOut(traveldates, nottranels [][2]string) [][2]string {

	addDate := func(date string, d int) string {
		t, _ := time.Parse("2006-01-02", date)
		return t.AddDate(0, 0, d).Format("2006-01-02")
	}

ReDo:
	backdates := make([][2]string, 0, len(traveldates)+3)
	NoAcross := false
	for _, traveldate := range traveldates {
		done := false
		for _, nottravel := range nottranels {
			if nottravel[1] < traveldate[0] || nottravel[0] > traveldate[1] {
				continue
			}
			done = true
			NoAcross = true
			//All Operation Take traveldate In nottravel
			if nottravel[0] > traveldate[0] && nottravel[0] >= traveldate[1] { //Left Across
				backdates = append(backdates, [2]string{traveldate[0], addDate(nottravel[0], -1)})

			} else if nottravel[0] <= traveldate[0] && nottravel[1] < traveldate[1] { //Right Across
				backdates = append(backdates, [2]string{addDate(nottravel[1], 1), traveldate[1]})

			} else if nottravel[0] > traveldate[0] && nottravel[1] <= traveldate[1] { //Out Include
				backdates = append(backdates, [2]string{traveldate[0], addDate(nottravel[0], -1)})
				if nottravel[1] < traveldate[1] {
					backdates = append(backdates, [2]string{addDate(nottravel[1], 1), traveldate[1]})
				}
			} //else if nottravel[0] > traveldate[0] && nottravel[1] > traveldate[1] { //In Include

			break
		}

		if !done {
			backdates = append(backdates, traveldate)
		}
	}

	if NoAcross { //多次的循环,把有可能包含的都去掉
		traveldates = backdates
		goto ReDo
	}

	return backdates
}

//把WeekLimit解析到兼容的数字格式
func AcrossWeek(WeekLimit string) [][2]int {
	weeks := strings.Split(WeekLimit, " ")
	var wk [][2]int

	start, end, old := 0, 0, 0

	for _, v := range weeks {
		if start == 0 {
			start = errorlog.GetWeekDay(v)
			old = start
		} else {
			end = errorlog.GetWeekDay(v)

			if end-old != 1 {
				wk = append(wk, [2]int{start, old})
				start, old = end, end
			} else {
				old = end
			}
		}
	}

	if start != 0 {
		return append(wk, [2]int{start, old})
	} else {
		return [][2]int{{1, 7}}
	}
}


//在接口返回的内容中找出可以的航线
func ApplyRoutine(sl string, depart, arrive, airline string) string {
	//airline是没有指定航司时默认调用进去的

	//把前面的内容截除掉,因为后把后面的内容的空格去除,然后把环行的内容加起来.
	if pos1 := strings.Index(sl, "1*"); pos1 > 0 {
		sl = sl[pos1:]
	} else {
		return ""
	}

	sl = strings.Replace(sl, " ", "", -1)
	//sl = strings.Replace(sl, "\n/", "/", -1)

	var ret1 string
	var ret2 string
	var tmp string

	for i := 1; i <= 20; i++ {
		pos1 := strings.Index(sl, strconv.Itoa(i)+"*")
		if pos1 == -1 {
			break
		}

		//pos2 := strings.IndexAny(sl[pos1:], "\\\r\n")
		pos2 := strings.IndexAny(sl[pos1:], "\n")
		if pos2 < 11 {
			break
		}

		str := sl[pos1+2 : pos1+pos2]
		stations := strings.Split(str, "-")
		n := strings.Count(str, "-")

		if n == 1 { //无指定航司直飞
			if stations[0] == depart && stations[1] == arrive {
				tmp = depart + "-" + airline + "-" + arrive
				if len(ret1) == 0 {
					ret1 = tmp
				} else {
					//ret1 += "$" + tmp
				}
				if len(ret2) > 180 {
					break
				}
			}

		} else if n == 2 { //直飞
			if stations[0] == depart && stations[2] == arrive {
				if len(stations[1]) == 3 { //这里其实时一个机场
					tmp = depart + "-" + airline + "-" + stations[1] + "-" + airline + "-" + arrive
					if len(ret2) == 0 {
						ret2 = tmp
					} else {
						//ret2 += "$" + tmp
					}
					if len(ret2) > 180 {
						break
					}
				} else {
					if len(strings.Split(stations[1], "/")[0]) != 2 {
						continue
					}

					tmp = depart + "-" + stations[1] + "-" + arrive
					if len(ret1) == 0 {
						ret1 = strings.Replace(str, "/", " ", -1) //在ks中'/'录入的代表是不同航段的航司,这里是可选择航司.
					} else {
						//ret1 += "$" + strings.Replace(str, "/", " ", -1)
					}
					if len(ret1) > 180 {
						break
					}
				}
			}

		} else if n == 4 { //一次中转

			if !strings.Contains(stations[0], depart) || !strings.Contains(stations[4], arrive) {
				continue
			}

			tmp = ""
			for _, connect := range strings.Split(stations[2], "/") {
				if len(connect) != 3 {
					continue
				}

				if len(strings.Split(stations[1], "/")[0]) != 2 {
					continue
				}

				if connect == depart || connect == arrive {
					tmp01 := depart + "-" + stations[1] + "-" + arrive
					if len(ret1) == 0 {
						ret1 = strings.Replace(tmp01, "/", " ", -1) //在ks中'/'录入的代表是不同航段的航司,这里是可选择航司.
					} else {
						//ret1 += "$" + strings.Replace(tmp01, "/", " ", -1)
					}
					continue
				}

				if len(tmp) == 0 {
					tmp = depart + "-" + stations[1] + "-" + connect + "-" + stations[3] + "-" + arrive
				} else {
					//tmp += "$" + depart + "-" + stations[1] + "-" + connect + "-" + stations[3] + "-" + arrive
				}
			}

			if len(tmp) == 0 {
				continue
			}

			if len(ret2) == 0 {
				ret2 = strings.Replace(tmp, "/", " ", -1) //在ks中'/'录入的代表是不同航段的航司,这里是可选择航司.
			} else {
				//ret2 += "$" + strings.Replace(tmp, "/", " ", -1)
			}
			if len(ret2) > 180 {
				break //字段保存长度为200
			}
		}
	}

	ret := ""
	ok := false
	var departs, connects, arrives []string
	if len(ret1) > 0 {
		stations := strings.Split(ret1, "-")
		if departs, ok = cachestation.CityCounty[stations[0]]; !ok {
			departs = []string{stations[0]}
		}
		if arrives, ok = cachestation.CityCounty[stations[2]]; !ok {
			arrives = []string{stations[2]}
		}
		for _, depart := range departs {
			for _, arrive := range arrives {
				if ret == "" {
					ret = depart + "-" + stations[1] + "-" + arrive
				} else {
					ret += "$" + depart + "-" + stations[1] + "-" + arrive
				}
			}
		}
	} else {
		stations := strings.Split(ret2, "-")
		if departs, ok = cachestation.CityCounty[stations[0]]; !ok {
			departs = []string{stations[0]}
		}
		if connects, ok = cachestation.CityCounty[stations[2]]; !ok {
			connects = []string{stations[2]}
		}
		if arrives, ok = cachestation.CityCounty[stations[4]]; !ok {
			arrives = []string{stations[4]}
		}
		for _, depart := range departs {
			for _, connect := range connects {
				for _, arrive := range arrives {
					if ret == "" {
						ret = depart + "-" + stations[1] + "-" + connect + "-" + stations[3] + "-" + arrive
					} else {
						ret += "$" + depart + "-" + stations[1] + "-" + connect + "-" + stations[3] + "-" + arrive
					}
				}
			}
		}
	}
	return ret
}


//减少句子种的空格
func reducePlace(str string) string {
	tmpstr := make([]rune, 0, len(str))
	hs := false
	for _, b := range str {
		if b == ' ' {
			if !hs {
				hs = true
				tmpstr = append(tmpstr, b)
			}
		} else {
			hs = false
			tmpstr = append(tmpstr, b)
		}
	}
	return string(tmpstr)
}

//在接口返回的内容中找出使用的舱位
func ApplyCabin(xs string) string {
	var (
		cabin1 string
		cabin2 string
		//depart       string
		maincabin    bool
		othercabin   bool
		othercabinif bool
		//prefix       bool
	)

	for _, str := range strings.Split(xs, "\n") {
		str = strings.Trim(str, "\r ")

		if len(str) < 10 {
			continue
		}

		//str = reducePlace(str)

		if str[:3] == "---" {
			maincabin = true
			continue
		}

		if maincabin {
			maincabin = false
			substr := strings.Split(str, " ")
			cabin1 = substr[len(substr)-1]
		}

		if str[:7] == "BOOKING" {
			othercabin = true
			continue
		}

		if othercabin {
			str = reducePlace(str)
			substr := strings.Split(str, " ")
			Len := len(substr)
			if Len >= 3 && substr[0] == "VIA" &&
				(len(substr[2]) == 1 || substr[2][1] == '/') {
				//othercabin = false
				othercabinif = false
				if (len(cabin2) == 0 || strings.Index(str, "PERMITTED") >= 0) && cabin1 != substr[2][:1] {
					cabin2 = substr[2][:1]
				}
				continue
			} else if substr[0] == "IF" {
				othercabinif = true
				continue
			}

			if othercabinif && Len >= 4 &&
				(strings.Index(str, "REQUIRED") >= 0 || strings.Index(str, "PERMITTED") >= 0) {
				othercabinif = false
				if (len(substr[0]) == 1 || substr[0][1] == '/') &&
					len(cabin2) == 0 && cabin1 != substr[0][:1] {
					cabin2 = substr[0][:1]
					if strings.Index(str, "PERMITTED") >= 0 {
						break
					}
				}
			}
		}
	}

	if len(cabin1) == 0 || len(cabin2) == 0 {
		return ""
	}

	//if prefix {
	return cabin2 + "/" + cabin1
	//}
	//return cabin1 + "/" + cabin2
}







//删除的守护进程（这个方法是在main.go里面直接调用的）
//这里会循环调用。只要是晚上23点的时候，这个函数就会调用
func Daemon() {
	for {

		//睡眠一小时
		time.Sleep(time.Hour)

		if time.Now().Hour() == 23 {

			stopAccept = true
			time.Sleep(time.Minute) //停止一分钟,让接受的处理完

			QueryDepart.mutex.RLock()
			for _, Station := range QueryDepart.DepartStation {
				Station.mutex.RLock()
				for _, Arrive := range Station.ArriveStation {
					Arrive.gdsfare = make(map[string][]byte, 20)
				}
				Station.mutex.RUnlock()
			}
			QueryDepart.mutex.RUnlock()

			QueryArrive.mutex.RLock()
			for _, Station := range QueryArrive.DepartStation {
				Station.mutex.RLock()
				for _, Arrive := range Station.ArriveStation {
					Arrive.gdsfare = make(map[string][]byte, 20)
				}
				Station.mutex.RUnlock()
			}
			QueryArrive.mutex.RUnlock()
			stopAccept = false
		}
	}
}

/*************b2fare加载****************/

//缓存Fare(b2Fare/gdsFare)信息
func b2fareCache(QS *QueryStation,
	DepartStation string,
	ArriveStation string,
	fare *mysqlop.MainBill,
	isGDS bool) {

	QS.mutex.RLock()
	departother, ok := QS.DepartStation[DepartStation]
	QS.mutex.RUnlock()

	if !ok {
		departother = &OtherStation{ArriveStation: make(map[string]*DatesFares)}
		QS.mutex.Lock()
		QS.DepartStation[DepartStation] = departother
		QS.mutex.Unlock()
	}

	departother.mutex.RLock()
	datesfares, ok := departother.ArriveStation[ArriveStation]
	departother.mutex.RUnlock()

	if !ok {
		datesfares = &DatesFares{
			b2fare:  make(mysqlop.ListMainBill, 0, 20),
			gdsfare: make(map[string][]byte, 5),
			queue:   make(map[string]*FareQueue, 5),
		}
		departother.mutex.Lock()
		departother.ArriveStation[ArriveStation] = datesfares
		departother.mutex.Unlock()
	}

	if isGDS {
		//插入GDS
		datesfares.InsertGDS(fare)
	} else {
		//插入FB  票单
		datesfares.InsertFB(fare)
	}
}


//加载b2Fare数据,WebAPI Interface
func MainBillLoad(BillID string) {

	sqlselect := `Select BillID,Type,Provider,AirInc,FirstTransfer,SecondTransfer,BillAttribute,Explain_1,
			BillBerth,Springboard,TransferCity,Destination,E_Bill,IsStoredBill,AirIncBillPriceNO,Berth,FareBase,
			AdultsPrice,ChildrenPrice,ApplyHumen,Trip,SplitTripID,GoorBack,ApplyAir,NotFitApplyAir,Mark1,Mark2,
			ChangeDateChargeGo,ChangeDateChargeBack,ReturnTicketChargeAll,ReturnTicketChargePart,
			ConditionID,CommendModel,CommendDetail,MixBerth,MixBerthType,ShareDate,
			TravelFirstDate,TravelLastDate,ReserveFirstDate,ReserveLastDate,MinStay,MaxStay,
			WeekFirst,WeekLast,OutBill1,OutBill2,BackFlag,ChangeFlag,BaggageFlag,OperatingAirline,
			BigCustomerCode,OperateDateTime
		From MainBill Where TravelLastDate>='` + errorlog.Today() + "'"

	if len(BillID) > 0 {
		sqlselect += " And BillID='" + BillID + "'"
	}



	//舱位逆转
	reverseBerth := func(cabin string) string {
		cabins := []byte(cabin)
		for from, to := 0, len(cabins)-1; from < to; from, to = from+1, to-1 {
			cabins[from], cabins[to] = cabins[to], cabins[from]
		}
		return string(cabins)
	}


	conn, err := mysqlop.Connect()
	if err != nil {
		return
	}
	defer conn.Close()

	row, b := mysqlop.MyQuery(conn, sqlselect)
	if !b {
		return
	}
	defer row.Close()

	var bill *mysqlop.MainBill
	var OperatingAirline string
	gomb := make(map[int]*mysqlop.MainBill, 100000)   //int==SplitTripID
	backmb := make(map[int]*mysqlop.MainBill, 100000) //int==SplitTripID

	for row.Next() {
		bill = new(mysqlop.MainBill)

		if err := row.Scan(&bill.BillID, &bill.Type, &bill.Provider, &bill.AirInc, &bill.FirstTransfer, &bill.SecondTransfer, &bill.BillAttribute, &bill.Explain,
			&bill.BillBerth, &bill.Springboard, &bill.TransferCity, &bill.Destination, &bill.E_Bill, &bill.IsStoredBill, &bill.NeiBuWangID, &bill.Berth, &bill.FareBase,
			&bill.AdultsPrice, &bill.ChildrenPrice, &bill.ApplyHumen, &bill.Trip, &bill.SplitTripID, &bill.GoorBack, &bill.ApplyAir, &bill.NotFitApplyAir, &bill.Mark1, &bill.Mark2,
			&bill.ChangeDateChargeGo, &bill.ChangeDateChargeBack, &bill.ReturnTicketChargeAll, &bill.ReturnTicketChargePart,
			&bill.ConditionID, &bill.CommendModel, &bill.CommendDetail, &bill.MixBerth, &bill.MixBerthType, &bill.ShareDate,
			&bill.TravelFirstDate, &bill.TravelLastDate, &bill.ReserveFirstDate, &bill.ReserveLastDate, &bill.MinStay, &bill.MaxStay,
			&bill.WeekFirst, &bill.WeekLast, &bill.OutBill1, &bill.OutBill2, &bill.BackFlag, &bill.ChangeFlag, &bill.BaggageFlag, &OperatingAirline,
			&bill.BigCustomerCode, &bill.OperateDateTime); err == nil {

			bill.ID = getID()
			bill.Agency = "FB"
			if strings.Contains(bill.Mark1, "GV2") {
				bill.NumberOfPeople = 2
			} else {
				bill.NumberOfPeople = 1
			}
			if bill.PCC == "" {
				bill.PCC = "CAN131"
			}

			if len(bill.OperateDateTime) > 10 {
				bill.OperateDateTime = bill.OperateDateTime[:10] //后面时间不要
			}

			if len(OperatingAirline) > 0 {
				tss := strings.Split(OperatingAirline, "/")
				if len(bill.TransferCity) > 0 {
					if bill.GoorBack == 0 {
						bill.FirstTransfer = tss[0]
						if len(tss) == 2 {
							bill.SecondTransfer = tss[1]
						} else {
							bill.SecondTransfer = tss[0]
						}
					} else {
						bill.SecondTransfer = tss[0]
						if len(tss) == 2 {
							bill.FirstTransfer = tss[1]
						} else {
							bill.FirstTransfer = tss[0]
						}
					}
				} else {
					bill.FirstTransfer = tss[0]
				}
			}

			if bill.GoorBack == 1 && len(bill.Berth) >= 3 {
				bill.Berth = reverseBerth(bill.Berth)
			}

			if bill.TransferCity == "" || bill.Springboard == bill.TransferCity || bill.TransferCity == bill.Destination {
				bill.Routine = bill.Springboard + "-" + bill.FirstTransfer + "-" + bill.Destination
			} else {
				bill.Routine = bill.Springboard + "-" + bill.FirstTransfer + "-" + bill.TransferCity + "-" + bill.SecondTransfer + "-" + bill.Destination
			}

			if bill.Trip == 1 {
				if bill.GoorBack == 0 {
					gomb[bill.SplitTripID] = bill
				} else {
					backmb[bill.SplitTripID] = bill
				}
			}

			b2fareCache(&QueryDepart, bill.Springboard, bill.Destination, bill, false)
			b2fareCache(&QueryArrive, bill.Destination, bill.Springboard, bill, false)
		}
	}

	//把航班限制添加到去成,这样数据在拷贝的时候快
	for id, backmb := range backmb {
		if mb, ok := gomb[id]; ok {
			mb.Mark1 = backmb.ApplyAir
			mb.Mark2 = backmb.NotFitApplyAir
		}
	}
}

//b2Fare删除缓存函数,colText使用变量类型interface{}会是比较合理的.
func b2fareDelete(QS *QueryStation, Level int, colText, DepartStation, ArriveStation string) {

	QS.mutex.RLock()
	departother, ok := QS.DepartStation[DepartStation]
	QS.mutex.RUnlock()

	if !ok {
		return
	}

	departother.mutex.RLock()
	datesfares, ok := departother.ArriveStation[ArriveStation]
	departother.mutex.RUnlock() //下面的内容放进锁里会增加安全性,因为多进程些会导致问题。

	if !ok {
		return
	}

	datesfares.Delete(Level, colText)
}

//gdsFare根据命令编号删除缓存
func GdsFareCacheDelete(QS *QueryStation, CommandID int, DepartStations CSList, ArriveStations CSList) {
	command := strconv.Itoa(CommandID)

	for _, depart := range DepartStations {
		for _, arrive := range ArriveStations {
			b2fareDelete(QS, 1, command, depart.Airport, arrive.Airport)
		}
	}
}


//b2Fare根据票单号删除所有,WebAPI Interface
func MainBillDelete(BillID string) {
	sqlselect := "Select Distinct Springboard,Destination From MainBill Where BillID='" + BillID + "'"

	conn, err := mysqlop.Connect()
	if err != nil {
		return
	}
	defer conn.Close()

	row, b := mysqlop.MyQuery(conn, sqlselect)
	if !b {
		return
	}
	defer row.Close()

	var DepartStation, ArriveStation string

	for row.Next() {


		//同一条票单上面可能有多个出发地，多个目的地
		if err := row.Scan(&DepartStation, &ArriveStation); err == nil {


			b2fareDelete(&QueryDepart, 0, BillID, DepartStation, ArriveStation)
			b2fareDelete(&QueryArrive, 0, BillID, ArriveStation, DepartStation)
		}
	}
}


//gdsFare根据命令编号删除所有,WebAPI Interface（传入commandID，以及出发城市组，目的地城市组）
func GdsFareDelete(CommandID int, DepartStations CSList, ArriveStations CSList) error {
	if err := GDSCommandDelete(CommandID); err != nil {
		return err
	}

	GdsFareCacheDelete(&QueryDepart, CommandID, DepartStations, ArriveStations)
	GdsFareCacheDelete(&QueryArrive, CommandID, ArriveStations, DepartStations)
	return nil
}


func GdsFareQuery(CommandID int, DepartStations CSList, ArriveStations CSList) mysqlop.ListMainBill {
	mb := make(mysqlop.ListMainBill, 0, len(DepartStations)*len(ArriveStations)*7)

	for _, depart := range DepartStations {
		QueryDepart.mutex.RLock()
		departother, ok := QueryDepart.DepartStation[depart.Airport]
		QueryDepart.mutex.RUnlock()
		if !ok {
			continue
		}

		for _, arrive := range ArriveStations {
			departother.mutex.RLock()
			datesfares, ok := departother.ArriveStation[arrive.Airport]
			departother.mutex.RUnlock()
			if !ok {
				continue
			}

			for _, fare := range datesfares.b2fare {
				if fare.CommandID == CommandID {
					mb = append(mb, fare)
				}
			}
		}
	}

	return mb
}

/*************GDS(b3fare/现GDSFare)数据库操作*************/
//添加进数据库
func GDSFareInsert(conn *sql.DB, fare *outsideapi.GDSFare) error {

	if fare.Status == 4 || fare.Status == 0 {
		sqlDelete := "Delete From GDSFare Where Departure=? And Arrival=? And Airline=? And FareBase=? And CommandID<>?"
		if !mysqlop.MyExec(conn, sqlDelete, fare.Departure, fare.Arrival, fare.Airline, fare.FareBase, fare.CommandID) {

			errorlog.WriteErrorLog("GDSFareInsert (1): ")
			return errors.New("Exec Delete Error")
		}
	}


	sqlInsert := `Insert Into GDSFare(CommandID,FareType,Departure,Arrival,Airline,TripType,Currency,CommandStr,
		IndexID,TravelFirstDate,TravelLastDate,BookingClass,Cabin,FareBase,WeekLimit,MinStay,MaxStay,ApplyAir,NotFitApplyAir,
		AdultPrice,ChildPrice,GDS,Agency,PriceOrder,ApplyRoutine,TravelDate,BackDate,NotTravel,
		AdvpTicketing,AdvpResepvations,RemarkText,IsRt,FirstSalesDate,LastSalesDate,NumberOfPeople,Status,QueryDate,InsertDate)
		values(?,?,?,?,?,?,?,?,  ?,?,?,?,?,?,?,?,?,?,?,  ?,?,?,?,?,?,?,?,?, ?,?,?,?,?,?,?,  ?,?,?)`

	if !mysqlop.MyExec(conn, sqlInsert, fare.CommandID, fare.FareType, fare.Departure, fare.Arrival, fare.Airline, fare.TripType, fare.Currency, fare.CommandStr,
		fare.Index, fare.TravelFirstDate, fare.TravelLastDate, fare.BookingClass, fare.Cabin, fare.FareBase, fare.WeekLimit, fare.MinStay, fare.MaxStay, fare.ApplyAir, fare.NotFitApplyAir,
		fare.AdultPrice, fare.ChildPrice, fare.GDS, fare.Agency, fare.PriceOrder, fare.ApplyRoutine, fare.TravelDate, fare.BackDate, fare.NotTravel,
		fare.AdvpTicketing, fare.AdvpResepvations, fare.Remark, fare.IsRt, fare.FirstSalesDate, fare.LastSalesDate, fare.NumberOfPeople, fare.Status, fare.QueryDate, fare.InsertDate) {

		errorlog.WriteErrorLog("GDSFareInsert (2): ")
		return errors.New("Exec Insert Error")
	}

	return nil
}


//加载进缓存
func GDSFareLoad() error {
	sqlselect := `Select CommandID,FareType,Departure,Arrival,Airline,TripType,Currency,CommandStr,
		IndexID,TravelFirstDate,TravelLastDate,BookingClass,Cabin,FareBase,WeekLimit,MinStay,MaxStay,ApplyAir,NotFitApplyAir,
		AdultPrice,ChildPrice,GDS,Agency,PriceOrder,ApplyRoutine,TravelDate,BackDate,NotTravel,
		AdvpTicketing,AdvpResepvations,RemarkText,IsRt,FirstSalesDate,LastSalesDate,NumberOfPeople,InsertDate
		From GDSFare Where Status=0 And TravelLastDate>='` + errorlog.Today() + "'"

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	row, b := mysqlop.MyQuery(conn, sqlselect)
	if !b {
		return errors.New("Query Error")
	}
	defer row.Close()

	var gdsfare *outsideapi.GDSFare

	for row.Next() {
		gdsfare = new(outsideapi.GDSFare)
		gdsfare.FareCmdInfo = new(outsideapi.FareCmdInfo)

		if err = row.Scan(&gdsfare.CommandID, &gdsfare.FareType, &gdsfare.Departure, &gdsfare.Arrival, &gdsfare.Airline, &gdsfare.TripType, &gdsfare.Currency, &gdsfare.CommandStr,
			&gdsfare.Index, &gdsfare.TravelFirstDate, &gdsfare.TravelLastDate, &gdsfare.BookingClass, &gdsfare.Cabin, &gdsfare.FareBase, &gdsfare.WeekLimit, &gdsfare.MinStay, &gdsfare.MaxStay, &gdsfare.ApplyAir, &gdsfare.NotFitApplyAir,
			&gdsfare.AdultPrice, &gdsfare.ChildPrice, &gdsfare.GDS, &gdsfare.Agency, &gdsfare.PriceOrder, &gdsfare.ApplyRoutine, &gdsfare.TravelDate, &gdsfare.BackDate, &gdsfare.NotTravel,
			&gdsfare.AdvpTicketing, &gdsfare.AdvpResepvations, &gdsfare.Remark, &gdsfare.IsRt, &gdsfare.FirstSalesDate, &gdsfare.LastSalesDate, &gdsfare.NumberOfPeople, &gdsfare.InsertDate); err == nil {

			for _, bill := range Copy(gdsfare) {
				b2fareCache(&QueryDepart, bill.Springboard, bill.Destination, bill, false)
				b2fareCache(&QueryArrive, bill.Destination, bill.Springboard, bill, false)
			}
		}
	}

	return nil
}

//删除数据库数据
func GDSCommandDelete(CommandID int) error {
	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	sqlDelete := "Delete From GDSFare Where CommandID=?"
	b := mysqlop.MyExec(conn, sqlDelete, CommandID)

	if !b {
		return errors.New("Exec Delete Error With CommandID " + strconv.Itoa(CommandID))
	}

	return nil
}

//查询某一命令涉及到的出发地/目的地
func GDSCommandDepartArrive(CommandID int) ([][2]string, error) {
	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	sqlselect := `Select DISTINCT Departure,Arrival From GDSFare Where CommandID=? And Status=0`

	row, b := mysqlop.MyQuery(conn, sqlselect, CommandID)
	if !b {
		return nil, errors.New("Exec Query Error With CommandID " + strconv.Itoa(CommandID))
	}
	defer row.Close()

	rets := make([][2]string, 0, 10)
	for row.Next() {
		var ret1, ret2 string

		if err = row.Scan(&ret1, &ret2); err == nil {
			rets = append(rets, [2]string{ret1, ret2})
		}
	}

	return rets, nil
}



/***************RuleCondition***********/
//添加进数据库
func RuleConditionInsert(conn *sql.DB, record *RuleRecord) error {

	sqlDelete := "Delete From RuleCondition Where ID=?"
	b := mysqlop.MyExec(conn, sqlDelete, record.ID)

	if !b {
		errorlog.WriteErrorLog("RuleConditionInsert (1): ")
		return errors.New("Exec Delete Error")
	}

	sqlInsert := `Insert Into RuleCondition(ID,TextTranslate,TravelDate,BackDate,NotTravel,FlightApplication,FlightNoApplication,MinStay,MaxStay,OutBill2,AFTER1,BEFORE1,
		FirstSalesDate,LastSalesDate,IsRt,NumberOfPeople,ApplyRoutine,ApplyCabin,InsertDate) values(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

	b = mysqlop.MyExec(conn, sqlInsert, record.ID, record.TextTranslate, record.TravelDate, record.BackDate, record.NotTravel, record.FlightApplication, record.FlightNoApplication, record.MinStay, record.MaxStay, record.OutBill2,
		record.AFTER, record.BEFORE, record.FirstSalesDate, record.LastSalesDate, record.IsRt, record.NumberOfPeople, record.ApplyRoutine, record.ApplyCabin, record.InsertDate)

	if !b {
		errorlog.WriteErrorLog("RuleConditionInsert (2): ")
		return errors.New("Exec Insert Error")
	}

	return nil
}

//删除数据库数据
func RuleConditionDelete(ID string) error {
	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	sqlDelete := "Delete From RuleCondition Where ID=?"
	b := mysqlop.MyExec(conn, sqlDelete, ID)

	if !b {
		return errors.New("Exec Delete Error With ID " + ID)
	}
	return nil
}

//加载进缓存
func RuleConditionLoad() error {

	//Ruleondition 数据是比较多的。总共有9k多

	sqlselect := `Select ID,TextTranslate,TravelDate,BackDate,NotTravel,FlightApplication,FlightNoApplication,MinStay,MaxStay,
		OutBill2,AFTER1,BEFORE1,FirstSalesDate,LastSalesDate,IsRt,NumberOfPeople,ApplyRoutine,ApplyCabin,InsertDate
	From RuleCondition`

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	row, b := mysqlop.MyQuery(conn, sqlselect)
	if !b {
		return errors.New("Query Error")
	}
	defer row.Close()

	RuleCondition.mutex.Lock()
	defer RuleCondition.mutex.Unlock()

	for row.Next() {
		rr := &RuleRecord{}
		if err := row.Scan(&rr.ID, &rr.TextTranslate, &rr.TravelDate, &rr.BackDate, &rr.NotTravel, &rr.FlightApplication, &rr.FlightNoApplication,
			&rr.MinStay, &rr.MaxStay, &rr.OutBill2, &rr.AFTER, &rr.BEFORE, &rr.FirstSalesDate, &rr.LastSalesDate, &rr.IsRt,
			&rr.NumberOfPeople, &rr.ApplyRoutine, &rr.ApplyCabin, &rr.InsertDate); err == nil {
			RuleCondition.Condition[rr.ID] = rr
		}
	}

	return nil
}

//加载一条数据
func RuleConditionLoadLocalOne(ID string) (*RuleRecord, error) {
	sqlselect := `Select ID,TextTranslate,TravelDate,BackDate,NotTravel,FlightApplication,FlightNoApplication,MinStay,MaxStay,
		OutBill2,AFTER1,BEFORE1,FirstSalesDate,LastSalesDate,IsRt,NumberOfPeople,ApplyRoutine,ApplyCabin,InsertDate
	From RuleCondition Where ID=?`

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	row, b := mysqlop.MyQuery(conn, sqlselect, ID)
	if !b {
		return nil, errors.New("Query Error")
	}
	defer row.Close()

	if row.Next() {
		rr := &RuleRecord{}
		if err := row.Scan(&rr.ID, &rr.TextTranslate, &rr.TravelDate, &rr.BackDate, &rr.NotTravel, &rr.FlightApplication, &rr.FlightNoApplication,
			&rr.MinStay, &rr.MaxStay, &rr.OutBill2, &rr.AFTER, &rr.BEFORE, &rr.FirstSalesDate, &rr.LastSalesDate, &rr.IsRt,
			&rr.NumberOfPeople, &rr.ApplyRoutine, &rr.ApplyCabin, &rr.InsertDate); err == nil {
			return rr, nil
		}
	}
	return nil, errors.New("No Record")
}



//删除一条航线的Condition(删除内容后,重做会自动删除数据库)
func RuleConditionDeleteLine(DepartStation, ArriveStation, Airline string) error {
	dc := make([]string, 0, 200)

	key := DepartStation + ArriveStation + Airline
	RuleCondition.mutex.RLock()
	for k := range RuleCondition.Condition {
		//如果在RuleCondition.Condition中可以找到这个key的话，则将该key添加到dc这个切片里面去
		if k[:8] == key {
			dc = append(dc, k)
		}
	}
	RuleCondition.mutex.RUnlock()



	RuleCondition.mutex.Lock()
	for _, k := range dc {
		delete(RuleCondition.Condition, k)
	}
	RuleCondition.mutex.Unlock()

	return nil
}





















/************航司集团缓存(航司指定的是‘WE’)******************/

/**这个AirlineGroupWE 不懂什么意思*/
var AirlineGroupWE = map[string][]string{
	"AA": {"AA", "US"},
	"US": {"AA", "US"},
	"AF": {"AF", "KL"},
	"KL": {"AF", "KL"},
	"BA": {"BA", "IB"},
	"IB": {"BA", "IB"},
	"BR": {"BR", "B7"},
	"B7": {"BR", "B7"},
	"CA": {"CA", "ZH"},
	"ZH": {"CA", "ZH"},
	"CI": {"CI", "AE"},
	"AE": {"CI", "AE"},
	"CX": {"CX", "KA"},
	"KA": {"CX", "KA"},
	"CZ": {"CZ", "MF"},
	"MF": {"CZ", "MF"},
	"HU": {"HU", "CN"},
	"CN": {"HU", "CN"},
	"LH": {"LH", "LX", "OS"},
	"LX": {"LH", "LX", "OS"},
	"OS": {"LH", "LX", "OS"},
	"MU": {"MU", "FM"},
	"FM": {"MU", "FM"},
	"SQ": {"SQ", "MI"},
	"MI": {"SQ", "MI"},
	"UA": {"UA", "CO"},
	"CO": {"UA", "CO"},
}

/************Cabin(BillBerth)数据库操作******************/
//加载进缓存（OK）
func CabinLoad() error {

	//TODO Cabin这个表重的ABC和Code分别代表什么。数据库里面有6700多条记录，分别代表什么
	sqlselect := `Select ABC,Code From Cabin`

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	row, b := mysqlop.MyQuery(conn, sqlselect)
	if !b {
		return errors.New("Query Error")
	}
	defer row.Close()

	var (
		ABC  string
		Code string
	)
	for row.Next() {
		if err := row.Scan(&ABC, &Code); err == nil {
			Cabin[ABC] = Code
		}
	}

	return nil
}


/*************重要的命令解析流程**********************/
type CityStation struct {
	Airport    string //城市
	FlightLine int    //城市下面所有机场可飞的目的地
	Parse      bool   //是否分析得到
}

type CSList []*CityStation

func (this CSList) Len() int {
	return len(this)
}

func (this CSList) Less(i, j int) bool {
	return this[i].FlightLine > this[j].FlightLine
}

func (this CSList) Swap(i, j int) {
	this[i], this[j] = this[j], this[i]
}


var AllCityStation CSList
var AllCityHot map[string]int

func GetAllCityStation() {

	AllCityStation = make(CSList, 0, len(cachestation.CityCountry))
	AllCityHot = make(map[string]int, len(cachestation.CityCountry))

	for city := range cachestation.CityCountry {
		nc := 0
		for _, airport := range cachestation.CityCounty[city] {
			if flightlines, ok := cachestation.FlightRoutine[airport]; ok {
				nc += len(flightlines)
			}
		}

		AllCityStation = append(AllCityStation,
			&CityStation{Airport: city,
				FlightLine: nc})

		AllCityHot[city] = nc
	}

	sort.Sort(AllCityStation)
}


func CommandParseID(command string) (string, int, error) {
	coms := strings.Split(command, " ")
	comLen := len(coms)

	if comLen != 3 ||
		coms[0] != "select" &&
			coms[0] != "delete" &&
			coms[0] != "update" ||
		coms[1] != "-ID" {
		return "", 0, errors.New("Using: select/delete/update -ID IDNumber")
	}

	if i, err := strconv.Atoi(coms[2]); err != nil {
		return "", 0, errors.New("Using: select/delete/update -ID IDNumber; No Right IDNumber")
	} else {
		if i <= 0 {
			return "", 0, errors.New("Using: select/delete/update -ID IDNumber; Take IDNumber > 0")
		}

		return coms[0], i, nil
	}
}



/**

命令解析... CommandParse（）！！！！！将参数进行解析
get -depart -city HKG -arrive -city [LHR AKL CMB BKK TPE DXB] -date 2018-03-12 -airline CX
*/
func CommandParse(
	command string, //待分解的命令
	cityParse bool) ( //是否输出目的地组
	string, //命令名
	CSList, //出发地组
	CSList, //目的地组
	[]string, //航司组(现在是一个航司,也许以后是多个)
	string, //查询日期
	string, //Trip(如无指定,单双程各获取一次)
	string, //isnego
	string, //accountcode
	error) {

	coms := strings.Split(command, " ")
	comLen := len(coms)

	if comLen < 5 {
		return "", nil, nil, nil, "", "", "", "", errors.New("No Right Parameter")
	}

	if coms[0] != "get" {
		return "", nil, nil, nil, "", "", "", "", errors.New("Take command get ...")
	}

	var (
		goCity       []string //出发城市
		goStartIndex int
		goLong       int
		goCC         int //city=1,country=2
		goTop        int

		backCity       []string
		backStartIndex int
		backLong       int
		backCC         int //city=1,country=2
		backTop        int

		Airlines    = make([]string, 0, 1)
		TravelDate  = errorlog.Today()
		RouteType   string
		IsNego      = "false"
		AccountCode string

		gb          int  //go = 1,back=2
		alb         bool //airline
		date        bool //TravelDate  日期 是否有-date
		rtb         bool //RouteType  单程或者往返 是否有-routine
		top         bool //-top  -top 是否显示最近多少条
		isnego      bool //-isnego   ？？？
		accountcode bool //-accountcode
		list        bool //数组循环
	)

	for ki, param := range coms[1:] {
		if len(param) == 0 {
			continue
		}

		if gb == 0 && param != "-depart" && param != "-arrive" &&
			param != "-airline" && !alb &&
			param != "-date" && !date &&
			param != "-route" && !rtb &&
			param != "-isnego" && !isnego &&
			param != "-accountcode" && !accountcode {
			return "", nil, nil, nil, "", "", "", "", errors.New("Need Parameter -depart/-arrive")
		}

		switch param {
		case "-airline":
			alb = true
			gb = 9
		case "-date":
			date = true
			gb = 9
		case "-route":
			rtb = true
			gb = 9
		case "-depart":
			gb = 1
		case "-arrive":
			gb = 2
		case "-top":
			top = true
			gb = 9
		case "-isnego":
			isnego = true
			gb = 9
		case "-accountcode":
			accountcode = true
			gb = 9
		case "-city":
			if gb == 1 {
				goCC = 1
			} else {
				backCC = 1
			}
		case "-country":
			if gb == 1 {
				goCC = 2
			} else {
				backCC = 2
			}
		default:
			if !list && param[0] == '[' {
				list = true
				param = param[1:]
				coms[ki] = param
				if gb == 1 {
					goStartIndex = ki
				} else {
					backStartIndex = ki
				}
			}

			cLen := len(param)
			if list && cLen > 0 && param[cLen-1] == ']' {
				list = false
				param = param[:cLen-1]
				coms[ki] = param

				if gb == 1 {
					if goStartIndex == 0 {
						return "", nil, nil, nil, "", "", "", "", errors.New("-depart: Must Take '[' before ']'")
					}
					goLong = ki - goStartIndex + 1
				} else {
					if backStartIndex == 0 {
						return "", nil, nil, nil, "", "", "", "", errors.New("-arrive: Must Take '[' before ']'")
					}
					backLong = ki - backStartIndex + 1
				}
				continue
			}

			if list {
				continue
			}

			if alb {
				alb = false
				if len(param) != 2 {
					return "", nil, nil, nil, "", "", "", "", errors.New("Airline content error")
				}
				Airlines = append(Airlines, param)
				continue
			}

			if date {
				date = false
				if len(param) != 10 && param[4] != '-' && param[7] != '-' {
					return "", nil, nil, nil, "", "", "", "", errors.New("TravelDate content error")
				}

				if _, err := time.Parse("2006-01-02", param); err != nil {
					return "", nil, nil, nil, "", "", "", "", errors.New("TravelDate content error")
				}

				TravelDate = param
				continue
			}

			if rtb {
				rtb = false
				if param != "OW" && param != "RT" {
					return "", nil, nil, nil, "", "", "", "", errors.New("RouteType content error")
				}
				RouteType = param
				continue
			}

			if top {
				top = false
				tn, err := strconv.Atoi(param)

				if err != nil {
					return "", nil, nil, nil, "", "", "", "", err
				}

				if gb == 1 {
					goTop = tn
				} else {
					backTop = tn
				}
				continue
			}

			if gb == 1 {
				if goCity != nil {
					return "", nil, nil, nil, "", "", "", "", errors.New("-depart: Make station to array")
				}
				goCity = []string{param}
				continue
			} else if gb == 2 {
				if backCity != nil {
					return "", nil, nil, nil, "", "", "", "", errors.New("-arrive: Make station to array")
				}
				backCity = []string{param}
				continue
			}

			if isnego {
				isnego = false
				if param != "true" && param != "false" {
					return "", nil, nil, nil, "", "", "", "", errors.New("-isnego: Make isnego true or false")
				}
				IsNego = param
			}

			if accountcode {
				accountcode = false
				AccountCode = param
			}

		}
	}

	if goStartIndex > 0 {
		if goLong == 0 || goCity != nil {
			return "", nil, nil, nil, "", "", "", "", errors.New("-depart: parameter error,Array From '[' To ']' ")
		}

		goCity = coms[goStartIndex : goStartIndex+goLong]
	}

	if backStartIndex > 0 {
		if backLong == 0 || backCity != nil {
			return "", nil, nil, nil, "", "", "", "", errors.New("-arrive: parameter error,Array From '[' To ']' ")
		}

		backCity = coms[backStartIndex : backStartIndex+backLong]
	}

	if len(Airlines) == 0 {
		return "", nil, nil, nil, "", "", "", "", errors.New("-airline: Must Provide Airline Code ")
	}

	var goList, backList CSList
	if cityParse {
		goList = StationParse(goCC, goCity, goTop)
		backList = StationParse(backCC, backCity, backTop)
	}

	return coms[0], goList, backList, Airlines, TravelDate, RouteType, IsNego, AccountCode, nil
}

func StationParse(cc int, stations []string, top int) CSList {
	var CSs CSList

	if cc == 1 {
		CSs = make(CSList, 0, len(stations))
	} else {
		CSs = make(CSList, 0, 300)
	}

	for _, station := range stations {
		if station == "*" {
			if top > 0 && top < len(AllCityStation) {
				return AllCityStation[:top]
			} else {
				return AllCityStation
			}
		}

		Len := len(station)
		if Len < 2 || Len > 5 {
			continue
		}

		if station[0] == '[' { //对方录入时使用"[["
			station = station[1:]
			Len--
		}

		if station[Len-1] == ']' { // //对方录入时使用"]]"
			station = station[:Len-1]
			Len--
		}

		if cc == 1 {
			//if county, ok := cachestation.County[station]; ok {
			//	station = county.City
			//}使用机场就直接查询机场

			CSs = append(CSs,
				&CityStation{Airport: station,
					FlightLine: AllCityHot[station],
					Parse:      true})

		} else if cc == 2 {
			if citys, ok := cachestation.CountryCity[station]; ok {
				for _, city := range citys {
					CSs = append(CSs,
						&CityStation{Airport: city,
							FlightLine: AllCityHot[city]})
				}
			}
		}
	}

	if top > 0 && len(CSs) > top {
		sort.Sort(CSs)
		CSs = CSs[:top]
	}

	return CSs
}


func CommandSuper(ID int) (*MessQueue, error) {

	CommandCache.mutex.RLock()
	defer CommandCache.mutex.RUnlock()

	for mq, ok := CommandCache.Command[ID]; ok; mq, ok = CommandCache.Command[ID] {
		coms := strings.Split(mq.Command, " ")

		if len(coms) == 3 && coms[0] == "update" {
			ID, _ = strconv.Atoi(coms[2])
			continue
		}

		if len(coms) < 5 || coms[0] != "get" {
			return nil, errors.New("No Super On get Command")
		} else {
			return mq, nil
		}
	}

	return nil, errors.New("No Super On get Command")
}



//检查输入的命令。并按照空格分割开来。并判断第一个指令
func CommandCheck(Command string) (string, error) {
	coms := strings.Split(Command, " ")

	if coms[0] != "select" &&
		coms[0] != "delete" &&
		coms[0] != "update" &&
		coms[0] != "get" {
		return coms[0], errors.New("Using: select/delete/update/get ...")
	}

	if coms[0] == "get" {
		_, _, _, _, _, _, _, _, err := CommandParse(Command, false)
		return coms[0], err
	} else {
		_, _, err := CommandParseID(Command)
		return coms[0], err
	}
}





func CopyFareDeamon() {
	for {

		//休眠2小时。
		time.Sleep(time.Hour * 2)
		func() {
			defer errorlog.DealRecoverLog()
			if err := CheckOutFareData(); err != nil {
				errorlog.WriteErrorLog("CopyFareDeamon: " + err.Error())
			}
		}()
	}
}



//同步数据到外部电商

/**
重点方法。这里引入了一个新的数据库
10.205.4.178


*/

func CheckOutFareData() error {
	var (
		//ticketingAirline       string     //出票航司,1.不可为空 2.航空公司二字码 3.只能输入一个
		//operationAirlines      string     //同个集团的航空公司
		//originLand             string     //始发地,多个用“,”隔开 1.不得为空 2.可以填写：机场三字码”或“城市码” 3.最多允许100个机场三字码/城市码
		//destination            string     //目的地，多个用“,”隔开 1.不得为空 2.可以填写：机场三字码”或“城市码” 3.最多允许100个机场三字码/城市码
		cabin                  string //舱位， 用","表示航段的分割。 1、舱位代码。每段只允许录入一个舱位代码，若全程舱位一致则可以只录入一个
		validDate4Dep          string //去程旅行有效期，支持多段组合，用“,”隔开， 1.不得为空 2例：2014-04-01~2014-06-30，2014-09-01 ~2014-09-30， 3日期格式为 YYYY-MM-DD或YYYY/MM/DD，例：2014-04-01或2014/04/01
		validDate4Ret          string //回程旅行有效期，支持多段组合，用“,”隔开，
		saleDate               string //销售日期，1、不得为空 2.输入格式为：2014-04-01~2014-06-30 3.不支持多段组合， 4.3日期格式为 YYYY-MM-DD或YYYY/MM/DD，例：2014-04-01或20104/04/01
		adultPassengerIdentity = "A"  //成人旅客身份，1.不得为空 2.普通/学生 3.当输入学生时，儿童价格项输入无效 4.当为小团产品时，此适用身份类别必须为 普通。5、后期支持劳工、移民、海员、老人、青年
		ticketPrice            int    //销售票面价,1.不得为空 2.价格区间为【0-999999】 3、销售票面价为10的整数倍(向下取整，如录入3002，则实际录入数值为3000)
		childPrice             string //儿童价(可%)
		refundRule             string //退票规定,1、不可为空 2、可填写：收取20%退票费用，或者是收取500元退票费等。 3、退票规定最多为300个字符
		reissueRule            string //改期规定,1、不可为空 2、可填写：收取20%改期费用，或者是收取500元改期费等。 3、改期规定最多为300个字符
		noshowRule             string //误机罚金说明，1、不可为空 2、可填写：起飞前不得退票，不得改期 3、误机罚金说明最多为300个字符
		luggageRule            = "23" //行李额规定,1、不可为空2、可填写：1PC。逾重行李费用为每公斤100元3、行李额规定最多为300个字符
		//FlyRoute    string //飞行路线，可以填多个
		minStay string //最短停留期,1、 默认为空，代表无限制； 2、 格式为：数字+字符/字符 3D表示3天 ; 4M表示4个月 ; SAT表示周六; 3D/SAT表示3天或者周六 3、 12M 表示一年
		maxStay string //最长停留期,1、 默认为空，代表无限制； 2、 格式为：数字+字符/字符 3D表示3天 ; 4M表示4个月 ; SAT表示周六; 3D/SAT表示3天或者周六 3、 12M 表示一年
		//remark                 string     //备注,出票备注文本
		commission   float64    //代理费
		office       = "CAN131" //提供票价代理
		dataSource   = "FareV2" //票价来源GDS
		salesCountry = "CN"

		//处理字段
		tripType            string //航程种类，1、默认为直达；有直达和中转两个选项；2、不填写 默认为 直达
		restrictFlightNo    string //航班号限制,同一航段之间用，隔开表示或的关系；不同航段之间用/隔开
		excludeFlightNo     string //排除航班号限制，同一航段之间用，隔开表示或的关系；不同航段之间用/隔开。
		restrictFlightNoRet string
		excludeFlightNoRet  string

		sqlDelete = "Delete From FareV2"
		sqlInsert = `Insert Into FareV2(office,commission,dataSource,salesCountry,ticketingAirline,operationAirlines,
			originLand,destination,cabin,validDate4Dep,validDate4Ret,saleDate,adultPassengerIdentity,ticketPrice,childPrice,
			refundRule,reissueRule,noshowRule,luggageRule,FlyRoute,minStay,maxStay,remarkText,tripType,restrictFlightNo,
			excludeFlightNo,restrictFlightNoRet,excludeFlightNoRet,minTravelPerson,lateTicketingTimeLimit,DaysOfSale2Travel,
			CommandID,AirIncBillPriceNO,OperateDate) 
		values(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	)
	commission = 0.0

	getCondition16 := func(s1, s2, s3 string) string {
		if i := strings.Index(s1, "更改/取消附加费"); i > 0 {
			if j := strings.Index(s1[i+22:], s2); j > 0 {
				n := strings.Index(s1[i+22+j:], " (")
				m := strings.LastIndex(s1[i+22:i+22+j+n], "、")

				if m > 0 && n > 0 {
					return s1[i+25+m : i+22+j+n]
				} else if m > 0 {
					return s1[i+25+m:]
				}
			}
		}
		return s3
	}

	conn, err := mysqlop.MyConnect("wdswang", "wds@Yqf889", "10.205.4.178", "3306")
	if err != nil {
		return err
	}
	defer conn.Close()

	if !mysqlop.MyExec(conn, sqlDelete) {
		return errors.New("Exec Delete Error")
	}

	//记录可以混舱的数据
	mixCabin := make(map[string][]*mysqlop.MainBill, 100000) //string==mb.Remark+"@CommandID"

	departs := make([]string, 0, len(QueryArrive.DepartStation))
	QueryArrive.mutex.RLock()
	for station := range QueryArrive.DepartStation {
		departs = append(departs, station)
	}
	QueryArrive.mutex.RUnlock()

	for _, depart := range departs {
		QueryArrive.mutex.RLock()
		other := QueryArrive.DepartStation[depart]
		QueryArrive.mutex.RUnlock()

		arrives := make([]string, 0, len(other.ArriveStation))
		other.mutex.RLock()
		for station := range other.ArriveStation {
			arrives = append(arrives, station)
		}
		other.mutex.RUnlock()

		for _, arrive := range arrives {
			other.mutex.RLock()
			fares := other.ArriveStation[arrive]
			other.mutex.RUnlock()

			for _, mb := range fares.b2fare {
				if mb.Trip == 1 && mb.GoorBack == 1 {
					continue
				}

				//ticketingAirline = mb.AirInc
				//operationAirlines = mb.AirInc
				//originLand = mb.Springboard
				//destination = mb.Destination
				cabin = strings.Replace(mb.Berth, "/", ",", -1)
				validDate4Dep = mb.TravelFirstDate + "~" + mb.TravelLastDate
				if mb.Trip == 0 {
					validDate4Ret = ""
				} else {
					if mb.BillID == "FareV2" {
						validDate4Ret = mb.Provider + "~" + mb.BillAttribute
					} else {
						validDate4Ret = validDate4Dep //FB没分去程回程日期
					}
				}
				saleDate = mb.ReserveFirstDate + "~" + mb.ReserveLastDate
				//ticketPrice = mb.AdultsPrice
				childPrice = strconv.Itoa(mb.ChildrenPrice)
				//FlyRoute = mb.Routine
				minStay = strconv.Itoa(mb.MinStay)
				maxStay = strconv.Itoa(mb.MaxStay)
				//remark = mb.Remark //这里不适合FB

				if mb.Trip == 0 {
					tripType = "OW"
				} else {
					tripType = "RT"
				}
				restrictFlightNo = strings.Replace(mb.ApplyAir, " ", ",", -1)
				excludeFlightNo = strings.Replace(mb.NotFitApplyAir, " ", ",", -1)
				restrictFlightNoRet = strings.Replace(mb.Mark1, " ", ",", -1)
				excludeFlightNoRet = strings.Replace(mb.Mark2, " ", ",", -1)

				if mb.BillID == "FareV2" {
					dataSource = "FareV2"
					id := mb.Springboard + mb.Destination + mb.AirInc + strings.Replace(mb.FareBase, "/", "_", -1)
					RuleCondition.mutex.RLock()
					rule, ok := RuleCondition.Condition[id]
					RuleCondition.mutex.RUnlock()
					if ok && rule != nil && len(rule.TextTranslate) > 0 {
						refundRule = getCondition16(rule.TextTranslate, "退票", "16")
						reissueRule = getCondition16(rule.TextTranslate, "改期", "16")
					} else {
						refundRule = "16"
						reissueRule = "16"
					}

					if mb.Trip == 1 && len(mb.Remark) > 0 { //加入混舱适合条件
						k := mb.Remark + "@" + strconv.Itoa(mb.CommandID)
						if mbs, ok := mixCabin[k]; ok {
							mixCabin[k] = append(mbs, mb)
						} else {
							mixCabin[k] = []*mysqlop.MainBill{mb}
						}
					}
				} else {
					dataSource = "FB"
					refundRule = "16"
					reissueRule = "16"

					if mb.MixBerth == 1 {
						if mbs, ok := mixCabin[mb.BillID]; ok {
							mixCabin[mb.BillID] = append(mbs, mb)
						} else {
							mixCabin[mb.BillID] = []*mysqlop.MainBill{mb}
						}
					}
				}

				if !mysqlop.MyExec(conn, sqlInsert, office, commission, dataSource, salesCountry,
					mb.AirInc, mb.AirInc, mb.Springboard, mb.Destination, cabin, validDate4Dep, validDate4Ret,
					saleDate, adultPassengerIdentity, mb.AdultsPrice, childPrice, refundRule, reissueRule, noshowRule,
					luggageRule, mb.Routine, minStay, maxStay, mb.Remark, tripType, restrictFlightNo,
					excludeFlightNo, restrictFlightNoRet, excludeFlightNoRet, mb.NumberOfPeople, mb.OutBill1, mb.OutBill2,
					mb.CommandID, mb.NeiBuWangID, mb.OperateDateTime) {

					return errors.New("Exec Insert Error")
				}
			}
		}
	}

	//混舱操作
	tripType = "RT"
	for _, mbs := range mixCabin {
		if len(mbs) <= 1 {
			continue
		}

		for i := 0; i < len(mbs); i++ {
			for j := i + 1; j < len(mbs); j++ {
				mbi := mbs[i]
				mbj := mbs[j]
				if mbi.Berth == mbj.Berth || //相同舱位的不同日期
					mbi.BillBerth != mbj.BillBerth || //相同舱位等级
					mbi.Routine != mbj.Routine { //线路需要相同(直飞和中转不可以)
					continue
				}
				if mbi.BillID != "FareV2" &&
					mbi.ConditionID != mbj.ConditionID {
					continue //B2条款需要相同
				}

				if mbi.TravelFirstDate > mbj.Provider { //出发日期早的排前面
					mbi, mbj = mbj, mbi
				}
				cabin = strings.Replace(mbi.Berth, "/", ",", -1) + "," + errorlog.ReverseBerth(strings.Replace(mbj.Berth, "/", ",", -1))
				validDate4Dep = mbi.TravelFirstDate + "~" + mbi.TravelLastDate
				if mbi.BillID == "FareV2" {
					validDate4Ret = mbj.Provider + "~" + mbj.BillAttribute
				} else {
					validDate4Ret = mbj.TravelFirstDate + "~" + mbj.TravelLastDate
				}
				saleDate = mbi.ReserveFirstDate + "~" + mbi.ReserveLastDate
				ticketPrice = (mbi.AdultsPrice + mbj.AdultsPrice) / 2
				childPrice = strconv.Itoa((mbi.ChildrenPrice + mbj.ChildrenPrice) / 2)
				minStay = strconv.Itoa(mbi.MinStay)
				maxStay = strconv.Itoa(mbi.MaxStay)

				restrictFlightNo = strings.Replace(mbi.ApplyAir, " ", ",", -1)
				excludeFlightNo = strings.Replace(mbi.NotFitApplyAir, " ", ",", -1)
				restrictFlightNoRet = strings.Replace(mbj.Mark1, " ", ",", -1)
				excludeFlightNoRet = strings.Replace(mbj.Mark2, " ", ",", -1)

				if mbi.BillID == "FareV2" {
					dataSource = "FareV2"
					id := mbi.Springboard + mbi.Destination + mbi.AirInc + strings.Replace(mbi.FareBase, "/", "_", -1)
					RuleCondition.mutex.RLock()
					rule, ok := RuleCondition.Condition[id]
					RuleCondition.mutex.RUnlock()
					if ok && rule != nil && len(rule.TextTranslate) > 0 {
						refundRule = getCondition16(rule.TextTranslate, "退票", "16")
						reissueRule = getCondition16(rule.TextTranslate, "改期", "16")
					} else {
						refundRule = "16"
						reissueRule = "16"
					}
				} else {

					dataSource = "FB"
					refundRule = "16"
					reissueRule = "16"
				}

				if !mysqlop.MyExec(conn, sqlInsert, office, commission, dataSource, salesCountry,
					mbi.AirInc, mbi.AirInc, mbi.Springboard, mbi.Destination, cabin, validDate4Dep, validDate4Ret,
					saleDate, adultPassengerIdentity, ticketPrice, childPrice, refundRule, reissueRule, noshowRule,
					luggageRule, mbi.Routine, minStay, maxStay, mbi.Remark, tripType, restrictFlightNo,
					excludeFlightNo, restrictFlightNoRet, excludeFlightNoRet, mbi.NumberOfPeople, mbi.OutBill1, mbi.OutBill2,
					mbi.CommandID, mbi.NeiBuWangID, mbi.OperateDateTime) {

					return errors.New("Exec Insert Error")
				}
			}
		}
	}
	return nil
}














/***************RuleTranslate数据库操作***********/
//添加进数据库
/**
先删除旧数据，接着将新的赋值到里面去
*/
func RuleTranslateInsert(ID int, Item, Airline, EnColumn, CnColumn string) error {

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	sqlDelete := "Delete From RuleTranslate Where ID=?"
	b := mysqlop.MyExec(conn, sqlDelete, ID)

	if !b {
		return errors.New("Exec Delete Error")
	}

	sqlInsert := `Insert Into RuleTranslate(ID,Item,Airline,EnColumn,CnColumn) values(?,?,?,?,?)`

	b = mysqlop.MyExec(conn, sqlInsert, ID, Item, Airline, EnColumn, CnColumn)

	if !b {
		return errors.New("Exec Insert Error")
	}

	return nil
}

//删除数据库数据
func RuleTranslateDelete(ID int) error {
	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	sqlDelete := "Delete From RuleTranslate Where ID=?"
	b := mysqlop.MyExec(conn, sqlDelete, ID)

	if !b {
		return errors.New("Exec Delete Error With ID " + strconv.Itoa(ID))
	}

	return nil
}

/**
获取条款里面最大的ID
*/
func RuleTranslateMaxID() error {
	sqlselect := `Select Max(ID) From RuleTranslate`

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	row, b := mysqlop.MyQuery(conn, sqlselect)
	if !b {
		return errors.New("Query Error")
	}
	defer row.Close()

	var (
		ID int
	)

	if row.Next() {

		if err := row.Scan(&ID); err == nil {
			RuleID.ID = ID
		}
	}

	return nil
}


//加载进缓存
func RuleTranslateLoad() error {
	sqlselect := `Select ID,Item,Airline,EnColumn,CnColumn From RuleTranslate`

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	row, b := mysqlop.MyQuery(conn, sqlselect)
	if !b {
		return errors.New("Query Error")
	}
	defer row.Close()

	var (
		ID       int
		Item     string
		Airline  string
		EnColumn string
		CnColumn string
	)

	for row.Next() {

		if err := row.Scan(&ID, &Item, &Airline, &EnColumn, &CnColumn); err == nil {

			RuleTotalParse.AddRule(ID, Item, Airline, EnColumn, CnColumn)
		}
	}

	return nil
}

func QueryRule(Item, Airline string) (*bytes.Buffer, error) {
	sqlselect := `Select ID,EnColumn,CnColumn,Item From RuleTranslate Where Airline=?`

	if len(Item) > 0 {
		sqlselect += ` And Item=?`
	}

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var row *sql.Rows
	var b bool
	type RetMess struct {
		ID       int    `json:"ID"`
		EnColumn string `json:"EnColumn"`
		CnColumn string `json:"CnColumn"`
		Item     string `json:"Item"`
	}

	ret := make([]*RetMess, 0, 1000)

	if len(Item) > 0 {
		row, b = mysqlop.MyQuery(conn, sqlselect, Airline, Item)
	} else {
		row, b = mysqlop.MyQuery(conn, sqlselect, Airline)
	}

	if !b {
		return errorlog.Make_JSON_GZip_Reader(ret), errors.New("Query Error")
	}
	defer row.Close()

	var (
		ID       int
		EnColumn string
		CnColumn string
		Itemtmp  string
	)

	for row.Next() {
		if err := row.Scan(&ID, &EnColumn, &CnColumn, &Itemtmp); err == nil {

			ret = append(ret, &RetMess{
				ID:       ID,
				EnColumn: EnColumn,
				CnColumn: CnColumn,
				Item:     Itemtmp})
		}
	}

	return errorlog.Make_JSON_GZip_Reader(ret), nil
}



/***#TODO************Command数据库操作***********/

//添加进数据库
func CommandInsert(ID int, CommandStr string, Status int, Operator string, InsertTime string) error {

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	sqlDelete := "Delete From Command Where ID=?"
	b := mysqlop.MyExec(conn, sqlDelete, ID)

	if !b {
		return errors.New("Exec Delete Error")
	}

	sqlInsert := `Insert Into Command(ID,CommandStr,Status,Operator,OperateDateTime) values(?,?,?,?,?)`

	b = mysqlop.MyExec(conn, sqlInsert, ID, CommandStr, Status, Operator, InsertTime)

	if !b {
		return errors.New("Exec Insert Error")
	}

	return nil
}

//删除数据库数据
func CommandDelete(ID int) error {

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	sqlDelete := "Delete From Command Where ID=?"
	b := mysqlop.MyExec(conn, sqlDelete, ID)

	if !b {
		return errors.New("Exec Delete Command Error With ID " + strconv.Itoa(ID))
	}

	return nil
}


//获取最大的命令行ID
func CommandMaxID() error {
	sqlselect := `Select Max(ID) From Command`

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	row, b := mysqlop.MyQuery(conn, sqlselect)
	if !b {
		return errors.New("Query Error")
	}
	defer row.Close()

	var (
		ID int
	)

	//命令行加锁处理
	CommandCache.mutex.Lock()
	defer CommandCache.mutex.Unlock()


	if row.Next() {
		if err := row.Scan(&ID); err == nil {
			CommandID.ID = ID
		}
	}

	return nil
}

//更新命令行状态 status
func CommandUpdateID(ID, Status int) error {

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	sqlUpdate := "Update Command Set Status=? Where ID=?"
	b := mysqlop.MyExec(conn, sqlUpdate, Status, ID)

	if !b {
		return errors.New("Exec Update Command Error With ID " + strconv.Itoa(ID))
	}

	return nil
}



//加载进缓存(加载2部分数据)
func CommandLoad() error {

	sqlselect := `Select ID,CommandStr,Status,Operator,OperateDateTime From Command Order By ID`

	conn, err := mysqlop.LocalConnect()
	if err != nil {
		return err
	}
	defer conn.Close()

	row, b := mysqlop.MyQuery(conn, sqlselect)
	if !b {
		return errors.New("Query Error")
	}
	defer row.Close()


	//定义这个MessQueue是要处理什么？
	var (
		mq *MessQueue
	)

	CommandCache.mutex.Lock()
	defer CommandCache.mutex.Unlock()

	for row.Next() {
		mq = new(MessQueue)
		if err := row.Scan(&mq.ID, &mq.Command, &mq.Status, &mq.Operator, &mq.InsertTime); err == nil {
			CommandCache.Command[mq.ID] = mq
			if mq.Status != 3 {
				commMission.Insert(mq)
			}
		}
	}

	c := commMission.count
	if c > MaxCommandDeamon {
		c = MaxCommandDeamon
	}

	for i := 0; i < c; i++ {
		commMission.Exec()
	}

	return nil
}


//读取最近的中转地
var nearConnect = struct {
	ConnectStation map[string]string
	mutex          sync.RWMutex
}{
	ConnectStation: make(map[string]string, 2000),
}


//#TODO fare 获取两地之间距离最短的中转站（这个值是否会因为飞机的速度而改变）
//获取最短距离的中转站
func getMinMileConnect(DepartStation, ArriveStation string) string {
	var (
		mile int
		tm   = 30000
		ti   int
		tc   string
		cs   []string
		rout string
		ok   bool
	)


	//route 其实就是两地之间的三字码拼接
	if DepartStation > ArriveStation {
		rout = ArriveStation + DepartStation
	} else {
		rout = DepartStation + ArriveStation
	}


	nearConnect.mutex.RLock()
	tc, ok = nearConnect.ConnectStation[rout]
	nearConnect.mutex.RUnlock()


	//如果找到了两地之间的的中转，则直接返回出去。
	if ok {
		return tc
	}



	//通过各种比较，最终将tc，也就是两个城市之间最短距离的那个中转站找到了。将其返回出去。并存到缓存里面去
	defer func() {
		nearConnect.mutex.Lock()
		nearConnect.ConnectStation[rout] = tc
		nearConnect.mutex.Unlock()
	}()


	//例如CANBJS。。。。这里的cs其实是一个数组。里面包含着route这两个城市之间所有的中转
	cs = cachestation.Routine[rout]
	cachestation.MileLock.RLock()
	defer cachestation.MileLock.RUnlock()

	for _, c := range cs {


		//这里的ti其实是指出发地到中转地之间的距离
		if DepartStation > c {
			ti = cachestation.Mile[c+DepartStation]
		} else {
			ti = cachestation.Mile[DepartStation+c]
		}


		//mile 保存了出发地和中转地之间的距离
		if ti != 0 {
			mile = ti
		} else {
			continue
		}


		//这里的ti其实是中转地和目的地之间的距离
		if c > ArriveStation {
			ti = cachestation.Mile[ArriveStation+c]
		} else {
			ti = cachestation.Mile[c+ArriveStation]
		}

		//现在的mile存了出发地+中转  和 中转+目的  这两段的距离
		if ti != 0 {
			mile += ti
		} else {
			continue
		}

		//这里的tm，其实只是随便定义的一个值，用来比较使用的。每次只要有新的更小的值就会更新。现在这里tm就是代表最短的距离。而c就是那个对应的中转站。
		//所谓的最近。其实就是 (A+B)   +    (B+c)     最短
		if mile < tm {
			tm = mile
			tc = c
		}
	}
	return tc
}



