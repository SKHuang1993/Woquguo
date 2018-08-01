package fare

import (
	"errors"
	//"fmt"
	"sync"
	//"reflect"
	"errorlog"
	"strconv"
	"strings"
	"time"
)

/***********翻译的结构及基层缓存********************/


//翻译格式
type ruleItemTranslate struct {
	ID                             int    //编号
	Airline                        string //指定的航司
	EnFore, EnBack, CnFore, CnBack bool    //英文的去程，回程；中文的去程，回程
	Count                          int
	EnColumns                      []string //待翻译的英文格式
	CnColumns                      []string //待翻译的中文格式
}

//翻译处理对象
type RuleItemParse struct {
	rules map[string][]*ruleItemTranslate //string==Item   //利用string，将对应的ruleItemTranslate存起来
	mutex sync.RWMutex
}


//添加翻译规则
func (this *RuleItemParse) AddRule(ID int, Item, Airline, EnColumn, CnColumn string) error {

	if len(EnColumn) == 0 || len(CnColumn) == 0 {
		return errors.New("No Context")
	}

	if strings.Count(EnColumn, "{") != strings.Count(EnColumn, "}") ||
		strings.Count(CnColumn, "{") != strings.Count(CnColumn, "}") ||
		strings.Count(EnColumn, "{") != strings.Count(CnColumn, "{") {
		return errors.New("No Fit")
	}

	EnFore := EnColumn[0] == '{'
	EnBack := EnColumn[len(EnColumn)-1] == '}'
	CnFore := CnColumn[0] == '{'
	CnBack := CnColumn[len(CnColumn)-1] == '}'

	rule := &ruleItemTranslate{ID: ID, Airline: Airline,
		EnFore: EnFore, EnBack: EnBack,
		CnFore: CnFore, CnBack: CnBack,
		Count: strings.Count(CnColumn, "{")}

	EnColumn = strings.Replace(EnColumn, "}", "{", -1)
	CnColumn = strings.Replace(CnColumn, "}", "{", -1)

	for k, v := range strings.Split(EnColumn, "{") {
		if k%2 == 0 && len(v) > 0 {
			rule.EnColumns = append(rule.EnColumns, strings.TrimSpace(v))
		}
	}

	for k, v := range strings.Split(CnColumn, "{") {
		if k%2 == 0 && len(v) > 0 {
			rule.CnColumns = append(rule.CnColumns, strings.TrimSpace(v))
		}
	}

	this.mutex.Lock()
	if this.rules == nil {
		this.rules = make(map[string][]*ruleItemTranslate, len(ItemParseFunc))
	}
	this.rules[Item] = append(this.rules[Item], rule)
	this.mutex.Unlock()
	return nil
}

//删除翻译规则
func (this *RuleItemParse) DeleteRule(ID int, Item string) error {
	this.mutex.RLock()
	rules, ok := this.rules[Item]
	this.mutex.RUnlock()

	if !ok {
		return errors.New("Not Item")
	}

	len := len(rules)
	for i := 0; i < len; i++ {
		if rules[i].ID == ID {
			rules[i], rules[len-1] = rules[len-1], rules[i]
			this.mutex.Lock()
			this.rules[Item] = rules[:len-1]
			this.mutex.Unlock()
			return nil
		}
	}

	return errors.New("Not Found")
}

//翻译句子
func (this *RuleItemParse) SentenceParse(text, Item, Airline string) (string, error) {
	columnsCount := 0
	index := 0
	var addContext []string
	addContextTmp := make([]string, 0, 10)

	this.mutex.RLock()
	rules := this.rules[Item]
	this.mutex.RUnlock()

	for i, rule := range rules {

		if rule.Airline != "" && rule.Airline != Airline {
			continue
		}

		if len(rule.EnColumns) == 1 && !rule.EnFore && !rule.EnBack {
			if rule.EnColumns[0] != text {
				continue
			} else {
				return rule.CnColumns[0], nil
			}
		}

		has := true
		aftertext := text
		for _, en := range rule.EnColumns {
			if strings.Index(text, en) < 0 {
				has = false
				break
			}
			aftertext = strings.Replace(aftertext, en, "#", 1)
		}

		addContextTmp := addContextTmp[:0]
		if has && len(rule.EnColumns) > columnsCount {
			for _, columns := range strings.Split(aftertext, "#") {
				tmp := strings.Trim(columns, " ")
				if strings.Count(tmp, " ") > 2 { //3个单词
					has = false //这里具备深度学习中成本函数的概念.
				}
				if len(tmp) > 0 {
					addContextTmp = append(addContextTmp, tmp)
				}
			}

			if has && len(addContextTmp) == rule.Count {
				columnsCount = len(rule.EnColumns)
				index = i
				addContext = addContextTmp
			}
		}

	}

	if columnsCount > 0 {
		rule := rules[index]
		retText := ""
		j := 0
		if rule.CnFore {
			retText += addContext[0] + " "
			j = 1
		}
		for i, v := range rule.CnColumns {
			retText += v + " "
			if (i + j) < len(addContext) {
				retText += addContext[i+j] + " "
			}
		}
		return retText, nil
	} else {
		return text, errors.New("Can't Translate")
	}
}

/************翻译时的分解过程*********************/

//翻译时起始关键字
var mainKey = map[string]struct{}{
	"NOTE": {},

	//第16项关键字  主要是关于退改签这几个的问题
	"CHANGES/CANCELLATIONS": {},
	"REROUTING":             {},
	"CHANGES":               {},
	"CANCELLATIONS":         {},
	"BEFORE DEPARTURE":      {},
	"AFTER DEPARTURE":       {},
}

//翻译时重复应用关键字
var repeatKey = map[string]struct{}{
	"ANY TIME": {},
}

//段落分割成句子
func phaseToSentences(phase string) []string {

	var sentences string
	var repeat string
	for _, sentence := range strings.Split(phase, "\n") {
		tmp := strings.TrimSpace(sentence)
		tmpLen := len(tmp)
		if tmpLen == 0 {
			continue
		} else {
			if tmp[tmpLen-1] == '-' {
				repeat = ""
				if tmp == "OUTBOUND -" || tmp == "INBOUND -" {
					tmp = tmp[:tmpLen-2] + "."
					tmpLen--
				} else {
					continue
				}
			}

			if tmpLen > 7 && tmp[:7] == "NOTE - " { //开头带NOTE -
				repeat = ""
				continue
			}

			if _, ok := mainKey[tmp]; ok {
				repeat = ""
				continue
			}

			if _, ok := repeatKey[tmp]; ok {
				repeat = tmp
				continue
			}

			if len(repeat) > 0 {
				tmp = repeat + " " + tmp
			}

			senLen := len(sentences)
			if senLen == 0 {
				sentences = tmp
			} else {
				if sentences[senLen-1] == '.' {
					sentences += tmp
				} else {
					sentences += " " + tmp
				}
			}
		}
	}

	senLen := len(sentences)
	if senLen > 0 {
		if sentences[senLen-1] == '.' {
			sentences = sentences[:senLen-1]
		}
	}

	if len(sentences) == 0 {
		return nil
	} else {
		sentences = strings.Replace(sentences, ".OR - ", ".OR.", -1)
		return strings.Split(sentences, ".")
	}
}

//默认的翻译过程
func defaultTranslate(itemText, Item, Airline string) ([]string, []string, error) {
	sentences := phaseToSentences(itemText)

	senLen := len(sentences)
	if senLen == 0 {
		return nil, nil, errors.New("No Sentence")
	}

	ret := make([]string, 0, senLen)
	for _, sentence := range sentences {
		if senTrans, err := RuleTotalParse.SentenceParse(sentence, Item, Airline); err == nil {
			ret = append(ret, senTrans)
		}
	}

	if len(ret) > 0 {
		return sentences, ret, nil
	} else {
		return sentences, nil, errors.New("Can't Translate")
	}
}

func defaultTranslateDate(itemText, Airline string) ([]string, []string, error) {
	var (
		cs, csOut, csIn   [][2]string
		OUTBOUND, INBOUND bool
		retcs             []string
	)

	sentences := phaseToSentences(itemText)
	for _, v := range sentences {
		if strings.Contains(v, "OUTBOUND") {
			OUTBOUND = true
			INBOUND = false
			continue
		} else if strings.Contains(v, "INBOUND") {
			OUTBOUND = false
			INBOUND = true
			continue
		}

		i := 0
		vLen := len(v)
		var s0, s1 string
		for pos := strings.Index(v[i:], " THROUGH "); pos > 5 && i+pos+13 <= vLen; pos = strings.Index(v[i:], " THROUGH ") {
			if v[i+pos-3] == ' ' { //DDMMM YY格式
				s0 = v[i+pos-8:i+pos-3] + v[i+pos-2:i+pos]
				s1 = v[i+pos+9:i+pos+14] + v[i+pos+15:i+pos+17]
				//cs = append(cs, [2]string{v[i+pos-8:i+pos-3] + v[i+pos-2:i+pos], v[i+pos+9:i+pos+14] + v[i+pos+15:i+pos+17]})
			} else { //DDMMM格式
				s0 = v[i+pos-5 : i+pos]
				s1 = v[i+pos+9 : i+pos+14]
				//cs = append(cs, [2]string{v[i+pos-5 : i+pos], v[i+pos+9 : i+pos+14]})
			}
			i += pos + 14

			if OUTBOUND {
				csOut = append(csOut, [2]string{s0, s1})
			} else if INBOUND {
				csIn = append(csIn, [2]string{s0, s1})
			} else {
				cs = append(cs, [2]string{s0, s1})
			}
		}
	}

	matchDate := func(cs [][2]string) []string {
		var ret []string
		today := errorlog.Today()
		month := today[5:7]
		year := today[2:4]
		nextyear := time.Now().AddDate(1, 0, 0).Format("2006")[2:]

		HadAdd1year := false //()如果中间的日期大于现在的前面,而前面的日期小于现在的日期,前面的日期不会是明年的日期.
		for i := len(cs); i > 0; i-- {
			if len(cs[i-1][0]) == 5 {
				if errorlog.GetMonthNum(cs[i-1][1][2:]) < month && !HadAdd1year {
					ret = append(ret, cs[i-1][0]+nextyear+"-"+cs[i-1][1]+nextyear)
				} else {
					HadAdd1year = true
					ret = append(ret, cs[i-1][0]+year+"-"+cs[i-1][1]+year)
				}
			} else {
				ret = append(ret, cs[i-1][0]+"-"+cs[i-1][1])
			}
		}
		return ret
	}

	if len(cs) > 0 {
		retcs = matchDate(cs)
	}
	if len(csOut) > 0 {
		retcs = append(retcs, "OUTBOUND")
		retcs = append(retcs, matchDate(csOut)...)
	}
	if len(csIn) > 0 {
		retcs = append(retcs, "INBOUND")
		retcs = append(retcs, matchDate(csIn)...)
	}

	return sentences, retcs, nil
}

//分析各项目的处理过程
//其实就是传一个string，后面加上一个函数。
var ItemParseFunc = map[string]func(string, string) ([]string, []string, error){
	"00": Item00,
	"02": Item02,
	"03": Item03,
	"04": Item04,
	"05": Item05,
	"06": Item06,
	"07": Item07,
	"08": Item08,
	"11": Item11,
	"13": Item13,
	"14": Item14,
	"15": Item15,
	"16": Item16,
	"18": Item18,
	"19": Item19}

var ItemName = map[string]string{
	"00": "适用性",
	"02": "适用日期/时间",
	"03": "适用季节",
	"04": "航班适用性",
	"05": "提前订位/出票",
	"06": "最短停留期",
	"07": "最长停留期",
	"08": "中途分程",
	"11": "不适用日期",
	"14": "旅行限制",
	"15": "销售限制",
	"16": "更改/取消附加费",
	"18": "机票签注",
	"19": "儿童/婴儿折扣"}

//适用性 (应用区域范围,其实这里已经被查询的出发地目的地划分了),*暂时不管
func Item00(itemText, Airline string) ([]string, []string, error) {
	return defaultTranslate(itemText, "00", Airline)
}

//适用日期/时间 ,02好像在航司条款中废弃了,而真实有效的是03
func Item02(itemText, Airline string) ([]string, []string, error) {
	return defaultTranslateDate(itemText, Airline)
}

//适用季节(适用日期)
func Item03(itemText, Airline string) ([]string, []string, error) {
	return defaultTranslateDate(itemText, Airline)
}

//航班适用性
func Item04(itemText, Airline string) ([]string, []string, error) { //[0]=Apply,[1]=NotApply
	fareValid := false                //Fare信息确认
	flightValid := false              //已经到了Flight信息
	jump := false                     //调转条信息
	mustNot := false                  //适合航班与不适合航班
	index := 0                        //处理到的位置
	var flightApplication []string    //返回的适合的航班
	var flightApplicationOA []string  ////返回的适合的航班中的OperatorAirline
	var flightNotApplication []string //返回的不适合的航班
	var flightNotApplicationOA []string
	var retInterface []string //[0]=Apply,[1]=NotApply(都包含OUTBOUND/INBOUND)
	//var OUTBOUND, INBOUND bool

	itemText += "\n" //为了可以直接进入循环
	textLen := len(itemText)
	pos := strings.IndexByte(itemText[index:], '\n')
	for pos > 0 {
		subText := strings.TrimSpace(itemText[index : index+pos])
		subLen := len(subText)

		if strings.Contains(subText, "OUTBOUND") {
			if mustNot {
				flightNotApplication = append(flightNotApplication, "OUTBOUND")
			} else {
				flightApplication = append(flightApplication, "OUTBOUND")
			}
			goto Next
		} else if strings.Contains(subText, "INBOUND") {
			if mustNot {
				flightNotApplication = append(flightNotApplication, "INBOUND")
			} else {
				flightApplication = append(flightApplication, "INBOUND")
			}
			goto Next
		}

		if subLen > 0 && subText[subLen-1] == '.' {
			subText = subText[:subLen-1]
			subLen--
		}

		if subLen == 0 {
			goto Next
		}

		if strings.Contains(subText, "S.W.") {
			jump = true
		}

		if strings.Contains(subText, "MUST NOT") {
			mustNot = true
		}

		if fareValid && !flightValid && strings.Contains(subText, "FLIGHT") {
			flightValid = true
		}

		if flightValid {
			flights := strings.Split(subText, " ")
			colLen := len(flights)

			if colLen == 3 && flights[0] != "ANY" && !jump {
				if flights[0] == Airline {
					if len(flights[2]) != 4 {
						flights[2] = strings.Repeat("0", 4-len(flights[2])) + flights[2]
					}

					if mustNot {
						flightNotApplication = append(flightNotApplication, flights[0]+flights[2])
					} else {
						flightApplication = append(flightApplication, flights[0]+flights[2])
					}
				}
			} else if colLen == 5 && !jump {
				if flights[0] == Airline {
					if len(flights[2]) != 4 {
						flights[2] = strings.Repeat("0", 4-len(flights[2])) + flights[2]
					}
					if len(flights[4]) != 4 {
						flights[4] = strings.Repeat("0", 4-len(flights[4])) + flights[4]
					}

					if mustNot {
						flightNotApplication = append(flightNotApplication, flights[0]+flights[2]+"-"+flights[4])
					} else {
						flightApplication = append(flightApplication, flights[0]+flights[2]+"-"+flights[4])
					}
				}
			} else if (colLen == 3 || colLen == 6) && flights[0] == "ANY" && !jump {
				if flights[1] == Airline {
					fs := flights[1]
					if colLen == 6 /*&& flights[1] != flights[5]*/ {
						//fs += "(" + flights[5] + ")"
						fs = flights[5] //这里获取的是承运航司
					}

					var OAs []string
					var ok bool
					if fs == "WE" {
						if OAs, ok = AirlineGroupWE[Airline]; !ok {
							OAs = []string{Airline}
						}
					} else {
						OAs = []string{fs}
					}

					if mustNot {
						for _, OA1 := range OAs {
							has := false
							for _, OA2 := range flightNotApplicationOA {
								if OA1 == OA2 {
									has = true
								}
							}
							if !has {
								flightNotApplicationOA = append(flightNotApplicationOA, OA1)
							}
						}
						//flightNotApplication = append(flightNotApplication, fs)
					} else {
						for _, OA1 := range OAs {
							has := false
							for _, OA2 := range flightApplicationOA {
								if OA1 == OA2 {
									has = true
								}
							}
							if !has {
								flightApplicationOA = append(flightApplicationOA, OA1)
							}
						}
						//flightApplication = append(flightApplication, fs)
					}
				}
			} else {
				flightValid = false
				jump = false
				mustNot = false
			}
		}

		if !fareValid && subLen > 3 && subText[:3] == "FSN" {
			fareValid = true
		}

		if !flightValid && subText == "ONE OR MORE OF THE FOLLOWING" {
			flightValid = true
		}

	Next:
		index += pos + 1
		if index >= textLen {
			break
		}

		pos = strings.IndexByte(itemText[index:], '\n')
	}

	if len(flightApplicationOA) > 0 {
		flightApplication = append(flightApplication, Airline+"("+strings.Join(flightApplicationOA, "\\")+")")
	}

	if len(flightNotApplicationOA) > 0 {
		flightNotApplication = append(flightNotApplication, Airline+"("+strings.Join(flightNotApplicationOA, "\\")+")")
	}

	retInterface = make([]string, 0, 2)
	if len(flightApplication) > 0 {
		retInterface = append(retInterface, strings.Join(flightApplication, " "))
	} else {
		retInterface = append(retInterface, "")
	}

	if len(flightNotApplication) > 0 {
		retInterface = append(retInterface, strings.Join(flightNotApplication, " "))
	} else {
		retInterface = append(retInterface, "")
	}

	return nil, retInterface, nil
}

//提前订位/出票(预订/出票)
func Item05(itemText, Airline string) ([]string, []string, error) {
	sentences := phaseToSentences(itemText)
	outbill2 := 365 //365==NULL

	for _, v := range sentences {
		i := 0
		vLen := len(v)
		for pos := strings.Index(v[i:], " AT LEAST "); pos != -1 && i+pos+15 <= vLen; pos = strings.Index(v[i:], " AT LEAST ") {
			j := 10
			for ; i+pos+j < vLen; j++ {
				if v[i+pos+j] == ' ' {
					break
				}
			}
			if day, err := strconv.Atoi(v[i+pos+10 : i+pos+j]); err == nil {
				if outbill2 > day {
					outbill2 = day
				}
			}
			i += pos + 15
		}
	}
	return sentences, []string{strconv.Itoa(outbill2)}, nil
}

//最短停留期
func Item06(itemText, Airline string) ([]string, []string, error) {
	sentences := phaseToSentences(itemText)

	minStay := "0"

	if len(sentences) == 0 || sentences[0] == "NO MINIMUM STAY REQUIREMENTS" {
		return sentences, []string{minStay}, nil
	}

ReDo:
	if pos := strings.Index(sentences[0], "NO EARLIER THAN "); pos != -1 {
		columns := strings.Split(sentences[0][pos+16:], " ")

		if len(columns) >= 2 {
			day, err := strconv.Atoi(columns[0])

			if err != nil {
				minStay = "6" //THE FIRST SUN第1个周日 Or 多少天的表达方式
				sentences[0] = sentences[0][pos+16:]
				goto ReDo
			} else {
				if columns[1] == "MONTHS" || columns[1] == "MONTH" {
					day *= 30
				}
			}
			minStay = strconv.Itoa(day)
		}
	}

	return sentences, []string{minStay}, nil
}

//最长停留期
func Item07(itemText, Airline string) ([]string, []string, error) {
	sentences := phaseToSentences(itemText)

	maxStay := "360"

	if len(sentences) == 0 || sentences[0] == "NO MAXIMUM STAY REQUIREMENTS" {
		return sentences, []string{"360"}, nil
	}

ReDo:
	if pos := strings.Index(sentences[0], "NO LATER THAN "); pos != -1 {
		columns := strings.Split(sentences[0][pos+14:], " ")

		if len(columns) >= 2 {
			day, err := strconv.Atoi(columns[0])

			if err != nil {
				sentences[0] = sentences[0][pos+14:]
				goto ReDo
			}

			if columns[1] == "MONTHS" || columns[1] == "MONTH" {
				day *= 30
			}
			maxStay = strconv.Itoa(day)
		}
	}

	return sentences, []string{maxStay}, nil
}

//中途分程**涉及到金额(中转一次加多少钱),但先不理
func Item08(itemText, Airline string) ([]string, []string, error) {
	return defaultTranslate(itemText, "08", Airline)
}

//不适用日期
func Item11(itemText, Airline string) ([]string, []string, error) {
	return defaultTranslateDate(itemText, Airline)
}

//最少人数
func Item13(itemText, Airline string) ([]string, []string, error) {
	sentences := phaseToSentences(itemText)
	NumberOfPeople := "1"

	for _, v := range sentences {
		i := 0
		vLen := len(v)

		if pos := strings.Index(v[i:], "MINIMUM "); pos != -1 && i+pos+8 < vLen {
			j := 9
			for ; i+pos+j < vLen; j++ {
				if v[i+pos+j] == ' ' {
					break
				}
			}
			NumberOfPeople = v[pos+8 : pos+j]
		}
	}
	return sentences, []string{NumberOfPeople}, nil
}

//旅行限制 TravelDate最后日期(现在认为一对AFTER/BEFOR是有效的,多对就是第3项的内容了)
func Item14(itemText, Airline string) ([]string, []string, error) {

	tda := func(b string) string {
		today := errorlog.Today()
		if errorlog.ChangeDate(b) >= today {
			return today[8:] + errorlog.GetMonthStr(today[5:7]) + today[2:4]
		} else {
			return ""
		}
	}

	tdb := func() string {
		today := errorlog.TodayAfterYear()
		return today[8:] + errorlog.GetMonthStr(today[5:7]) + today[2:4]
	}

	var AFTERBEFORE []string
	sentences := phaseToSentences(itemText)

	for _, v := range sentences {
		vLen := len(v)
		var AFTER, BEFORE string
		if pos := strings.Index(v, "AFTER "); pos != -1 && pos+13 < vLen && v[pos+11] == ' ' {
			AFTER = v[pos+6:pos+11] + v[pos+12:pos+14]
		}

		if pos := strings.Index(v, "BEFORE "); pos != -1 && pos+14 < vLen && v[pos+12] == ' ' {
			BEFORE = v[pos+7:pos+12] + v[pos+13:pos+15]
		}

		if len(AFTER) == 0 && len(BEFORE) == 7 {
			AFTER = tda(BEFORE)
		} else if len(AFTER) == 7 && len(BEFORE) == 0 {
			BEFORE = tdb()
		}

		if len(AFTER) == 7 && len(BEFORE) == 7 {
			AFTERBEFORE = append(AFTERBEFORE, AFTER+"-"+BEFORE)
			AFTER = ""
			BEFORE = ""
		}
	}

	return sentences, AFTERBEFORE, nil
}

//销售限制(不可以在某区域销售/最早销售日期/最晚销售日期)
func Item15(itemText, Airline string) ([]string, []string, error) {
	FirstSalesDate := errorlog.Today()
	LastSalesDate := errorlog.TodayAfterYear()

	sentences := phaseToSentences(itemText)

	for _, v := range sentences {
		vLen := len(v)
		if pos := strings.Index(v, "AFTER "); pos != -1 && pos+13 < vLen && v[pos+11] == ' ' {
			FirstSalesDate = errorlog.ChangeDate(v[pos+6:pos+11] + v[pos+12:pos+14])
		}

		if pos := strings.Index(v, "BEFORE "); pos != -1 && pos+14 < vLen && v[pos+12] == ' ' {
			LastSalesDate = errorlog.ChangeDate(v[pos+7:pos+12] + v[pos+13:pos+15])
		}
	}

	return sentences, []string{FirstSalesDate, LastSalesDate}, nil
}


//更改/取消附加费
func Item16(itemText, Airline string) ([]string, []string, error) {
	return defaultTranslate(itemText, "16", Airline)
}

//机票签注 担保?
func Item18(itemText, Airline string) ([]string, []string, error) {
	return defaultTranslate(itemText, "18", Airline)
}

//儿童/婴儿折扣
func Item19(itemText, Airline string) ([]string, []string, error) {
	return defaultTranslate(itemText, "19", Airline)
}

/**********Rule内容的翻译***********************/
//翻译后的内容保存结构
type RuleRecord struct {
	ID                  string `json:"-"` //Airline + DepartStation + ArriveStation + Rule/FareBase
	Airline             string `json:"-"`
	TextTranslate       string `json:"TextTranslate"`       //翻译后的所有文字表达内容
	TravelDate          string `json:"TravelDate"`          //旅行日期
	BackDate            string `json:"BackDate"`            //回程日期
	NotTravel           string `json:"NotTravel"`           //过滤掉的日期
	FlightApplication   string `json:"FlightApplication"`   //适合航班
	FlightNoApplication string `json:"FlightNoApplication"` //不适合航班
	MinStay             int    `json:"MinStay"`             //最小停留
	MaxStay             int    `json:"MaxStay"`             //最大停留
	OutBill2            int    `json:"AdvpResepvations"`    //预订提前天数,AdvpResepvations
	AFTER               string `json:"AFTER"`               //日期最小(旅行)
	BEFORE              string `json:"BEFORE"`              //日期最大
	FirstSalesDate      string `json:"FirstSalesDate"`      //最早销售日期(预订)
	LastSalesDate       string `json:"LastSalesDate"`       //最晚销售日期
	IsRt                int    `json:"-"`                   //是否混舱
	NumberOfPeople      int    `json:"NumberOfPeople"`      //最少人数
	InsertDate          string `json:"-"`                   //插入日期
	ApplyRoutine        string `json:"ApplyRoutine"`        //sl的应用航班
	ApplyCabin          string `json:"ApplyCabin"`          //xs的使用舱位
}

//FareBasic-->Rule
type Rule struct {
	*RuleRecord
	Item map[string]string //子项,是未被处理过后的.
}

//初始化,把Rule内容划分到各子项
func (this *Rule) Init(content, Airline string) error {

	//this.ID这里必须解决这个值
	this.Item = make(map[string]string, len(ItemParseFunc))
	this.RuleRecord = new(RuleRecord)
	this.Airline = Airline
	this.InsertDate = errorlog.Today()

	getItemContent := func(item string) (string, error) {
		item1 := "\n" + item + "."

		posBegin := strings.Index(content, item1)
		if posBegin < 0 {
			return "", errors.New("No Exist")
		}

		posBegin++ //PASS '\n'
		posBegin += strings.IndexByte(content[posBegin:], '\n') + 1

		itemi, _ := strconv.Atoi(item)
		posEnd := -1

		for itemi++; posEnd < 0 && itemi < 32; itemi++ {
			item2 := strconv.Itoa(itemi) + "."
			if len(item2) < 3 {
				item2 = "\n0" + item2
			} else {
				item2 = "\n" + item2
			}

			posEnd = strings.Index(content[posBegin:], item2)
		}

		if posEnd < 0 { //跳过最后的无效内容
			posEnd = strings.LastIndex(content[posBegin:], " END ")
			if posEnd > 0 {
				posEnd = strings.LastIndex(content[posBegin:posBegin+posEnd], "\n")
			}
		}

		text := ""
		if posEnd > 0 {
			text = content[posBegin : posBegin+posEnd]
		} else {
			text = content[posBegin:]
		}

		cs := strings.Split(text, "\n")
		text = ""
		for i := 0; i < len(cs); {
			if strings.Contains(cs[i], "RFSONLN") {
				i += 4
			} else {
				if i != len(cs) {
					text += cs[i] + "\n"
				} else {
					text += cs[i]
				}
				i++
			}
		}

		return text, nil
	}

	for k := range ItemParseFunc {
		if itemContent, err := getItemContent(k); err == nil {
			this.Item[k] = itemContent
		}
	}

	return nil
}

//获取子项的分段or段落.
func (this *Rule) subItem(item string) []string {
	if content, ok := this.Item[item]; !ok {
		return nil
	} else {
		var strList []string
		var strSubItem string
		index := 0
		cLen := len(content)
		if cLen == 0 {
			return nil
		}

		for {
			pos := strings.IndexByte(content[index:], '\n')
			if pos < 0 {
				strSubItem += content[index:]
				strList = append(strList, strSubItem)
				break
			}

			strSubItem += content[index : index+pos+1]
			if index+pos+1 < cLen {
				if content[index] == ' ' && content[index+pos+1] != ' ' && index != 0 {
					strList = append(strList, strSubItem)
					strSubItem = ""
				}
				index += pos + 1
			} else {
				strList = append(strList, strSubItem)
				break
			}
		}

		return strList
	}
}

//翻译各子项的第1各分段内容.
func (this *Rule) SubItemParse(item string) ([]string, []string, error) {
	fn, ok := ItemParseFunc[item]
	if !ok {
		return nil, nil, errors.New("No Proc Func")
	}

	i := 0 //00项是获取第2段内容,但其它都是获取第1段内容.
	if item == "00" {
		i = 1
	}
	itemtext := this.subItem(item)

	if item == "14" {
		return fn(strings.Join(itemtext, "."), this.Airline)
	} else if len(itemtext) == 0 || len(itemtext) < i {
		return fn("", this.Airline) //这里只是为了获取默认值
	} else {
		return fn(itemtext[i], this.Airline)
	}
}

//翻译主函数,把Rule内容翻译到RuleRecord结构里面(真实达到翻译的处理).
func (this *Rule) RuleParse() error {
	var itemcount int

	for _, item := range []string{"14", "03", "04", "05", "06", "07", "11", "00", "08", "13", "15", "16", "18", "19"} {
		if _, iList, err := this.SubItemParse(item); err == nil {

			switch item {
			case "03":
				if len(this.TravelDate) > 0 || len(this.BackDate) > 0 { //第14项已经解决
					continue
				}

				if len(iList) == 0 && len(this.AFTER) > 0 && len(this.BEFORE) > 0 {
					this.TravelDate = this.AFTER + "-" + this.BEFORE
					this.BackDate = this.AFTER + "-" + this.BEFORE
					continue
				}

				if len(iList) > 0 { //&& (len(this.AFTER) > 0 || len(this.BEFORE) > 0) {

					var (
						retOut, retIn []string
						after, befor  string
						out, in       bool
					)

					if len(this.AFTER) > 0 {
						after = errorlog.ChangeDate(this.AFTER)
					} else {
						after = errorlog.Today()
					}

					if after < errorlog.Today() {
						after = errorlog.Today()
					}

					if len(this.BEFORE) > 0 {
						befor = errorlog.ChangeDate(this.BEFORE)
					} else {
						befor = errorlog.TodayAfterYear()
					}

					//下面解决所有日期都在AFTER/BEFORE里面
					for i := 0; i < len(iList); i++ { //for i := len(iList) - 1; i >= 0; i-- {
						if iList[i] == "OUTBOUND" {
							out = true
							in = false
							continue
						} else if iList[i] == "INBOUND" {
							out = false
							in = true
							continue
						}

						if errorlog.ChangeDate(iList[i][8:]) < after || errorlog.ChangeDate(iList[i][0:7]) > befor {
							continue
						}
						//这里假设其它的都是包含在AFTER/BEFORE里面的,也就是说所有的TRAVELDATE都是适合的.
						//ret = append(ret, iList[i])
						if out {
							retOut = append(retOut, iList[i])
						} else if in {
							retIn = append(retIn, iList[i])
						} else {
							retOut = append(retOut, iList[i])
						}
					}

					if len(retOut) > 0 {
						this.TravelDate = strings.Join(retOut, " ")
					}
					if len(retIn) > 0 {
						this.BackDate = strings.Join(retIn, " ")
					}
				}
			case "04":
				this.FlightApplication = iList[0]
				this.FlightNoApplication = iList[1]
			case "05":
				if day, err := strconv.Atoi(iList[0]); err == nil {
					this.OutBill2 = day
				}
			case "06": //Fare本身数据有MinStay
				if min, err := strconv.Atoi(iList[0]); err == nil {
					this.MinStay = min
				}
			case "07": //Fare本身数据有MaxStay
				if max, err := strconv.Atoi(iList[0]); err == nil {
					this.MaxStay = max
				}
			case "11":
				this.NotTravel = strings.Join(iList, " ")
			case "13":
				if num, err := strconv.Atoi(iList[0]); err == nil {
					this.NumberOfPeople = num
				} else {
					this.NumberOfPeople = 1
				}
			case "14":
				if len(iList) == 1 {
					this.AFTER = iList[0][:7]
					this.BEFORE = iList[0][8:]
				} else if len(iList) > 1 {
					this.TravelDate = strings.Join(iList, " ")
					this.BackDate = strings.Join(iList, " ")
				}

			case "15":
				this.FirstSalesDate = iList[0]
				this.LastSalesDate = iList[1]
			case "00", "08", "16", "18", "19":
				if len(iList) > 0 {
					itemcount++
					this.TextTranslate += " (" + strconv.Itoa(itemcount) + ")、" + ItemName[item]

					for ik, iv := range iList {
						this.TextTranslate += " (" + strconv.Itoa(itemcount) + "." + strconv.Itoa(ik+1) + ")、" + iv
					}
				}
			}
		}
	}

	return nil
}
