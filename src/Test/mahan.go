package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	//	"time"
)

//联系人信息 ContactDTO
type ContactDTO struct {
	NameTitle         string `json:"nameTitle"`         //称谓: MR 先生 MS 女士 MISS 小姐
	GivenName         string `json:"givenName"`         //名称 FirstName+MidName
	Surname           string `json:"surname"`           //姓氏 LastName
	ContactName       string `json:"contactName"`       //联系人姓名，不区分姓和名
	Mobile            string `json:"mobile"`            //联系人移动号码
	AreaCityCode      string `json:"areaCityCode"`      //城市编号,如
	CountryAccessCode string `json:"countryAccessCode"` //国家编号
	Address           string `json:"address"`           //联系地址信息完整信息
	Postcode          string `json:"postcode"`          //邮编
	Email             string `json:"email"`             //电子邮箱

}

//乘客信息PassengerDTO
type PassengerDTO struct {
	PassengerType        string `json:"passengerType"`        //乘客类型 ADT 成人 CHD 儿童
	GivenName            string `json:"givenName"`            //名称 FirstName+MidName
	Surname              string `json:"surname"`              //姓氏 LastName
	NameTitle            string `json:"nameTitle"`            //称谓: MR 先生 MS 女士 MISS 小姐
	BirthDate            string `json:"birthDate"`            //生日 yyyyMMdd
	Gender               string `json:"gender"`               //性别(M 男性 F 女性)
	DocType              string `json:"docType"`              //证件类型(仅支持护照)
	DocNo                string `json:"docNo"`                //护照号码
	DocIssueCtry         string `json:"docIssueCtry"`         //护照发行国家(国家二字码) CN
	DocHolderNationality string `json:"docHolderNationality"` //护照持有者国籍(国家二字码) CN
	DocEffectiveDate     string `json:"docEffectiveDate"`     //护照生效日期 yyyyMMdd
	DocExpireDate        string `json:"docExpireDate"`        //护照失效日期 yyyyMMdd
	AreaCityCode         string `json:"areaCityCode"`         //城市编号,如 北京:010 上海:021   021
	CtryAccessCode       string `json:"ctryAccessCode"`       //国家编号 86
	Telephone            string `json:"telephone"`            //电话号码，可以是手机号或固定电话号码 Y
	AddrCtryCode         string `json:"addrCtryCode"`         //联系地址国家代码(国家二字码) CN
	AddrCityName         string `json:"addrCityName"`         //联系地址城市名称Shanghai

}

//价格校验 price

//search 查询接口返回的结构
type DepSegment struct {
	DepAirport      string `json:"depAirport"`
	ArrAirport      string `json:"arrAirport"`
	DepDate         string `json:"depDate"`
	ArrDate         string `json:"arrDate"`
	FlightNo        string `json:"flightNo"`
	Carrier         string `json:"carrier"`
	Rph             string `json:"rph"`
	JourneyDuration string `json:"journeyDuration"`
	CabinType       string `json:"cabinType"`
	CabinCode       string `json:"cabinCode"`
	returnFlag      bool   `json:"returnFlag"`
}
type RetSegment struct {
	DepAirport      string `json:"depAirport"`
	ArrAirport      string `json:"arrAirport"`
	DepDate         string `json:"depDate"`
	ArrDate         string `json:"arrDate"`
	FlightNo        string `json:"flightNo"`
	Carrier         string `json:"carrier"`
	Rph             string `json:"rph"`
	JourneyDuration string `json:"journeyDuration"`
	CabinType       string `json:"cabinType"`
	CabinCode       string `json:"cabinCode"`
	returnFlag      bool   `json:"returnFlag"`
}

type Routing struct {
	DepSegments []DepSegment `json:"depSegments"`
	RetSegments []RetSegment `json:"retSegments"`
	Status      string       `json:"status"`
	Currency    string       `json:"currency"`
	AdultFare   int          `json:"adultFare"`
	ChildFare   int          `json:"childFare"`
	AdultTax    int          `json:"adultTax"`
	ChildTax    int          `json:"childTax"`
	AdultFee    int          `json:"adultFee"`
	ChildFee    int          `json:"childFee"`
	TotalFare   int          `json:"totalFare"`
	AdultNum    int          `json:"adultNum"`
	ChildNum    int          `json:"childNum"`
	FareRule    string       `json:"fareRule"`
	FareRuleEN  string       `json:"fareRuleEN"`
	Data        string       `json:"data"`
}

type Result struct {
	Version      string    `json:"version"`
	Status       string    `json:"status"`
	ErrorCode    int       `json:"errorCode"`
	ErrorMsg     string    `json:"errorMsg"`
	SessionId    string    `json:"sessionId"`
	ResponseTime int       `json:"responseTime"`
	Routings     []Routing `json:"routings"`
}

func Search() {

	//baseUrl :="http://apiuat.iranmahanair.com/gsa-ws/mahan/search?"

	//version:="V1.2"
	//appId :="yiqifei"
	//privateKey := "0ff363f3-ebc0-4429-985e-05a5d990c830"
	////sessionId:=null
	//
	//depAirport:="PVG"
	//arrAirport:="DXB"
	//depDate:="20180226"
	//retDate:=":"
	//adultNum :="2"
	//childNum:="1"
	//preferCabin:="Y"

	body := bytes.NewBuffer([]byte(`{"adultNum":"1","appId":"yiqifei","arrAirport":"CDG","childNum":"0","depAirport":"CAN","depDate":"20180504","preferCabin":"Y","privateKey":"0ff363f3-ebc0-4429-985e-05a5d990c830","retDate":"20180510","version":"V1.2"}`))

	//fmt.Println(body)

	s := "http://apiuat.iranmahanair.com/gsa-ws/mahan/search"

	resp, err := http.Post(s, "application/json;charset=UTF-8", body)

	if err != nil {

		fmt.Println("错误了-----resp-")
		panic(err)
		return
	}

	defer resp.Body.Close()
	myresult, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		//fmt.Println("错误了-----result-")
		panic(err)
	}

	var result Result
	json.Unmarshal(myresult, &result)

	//	fmt.Println(string(myresult))
	fmt.Println(result)

}
