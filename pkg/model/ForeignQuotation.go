package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	clientInfluxdb "github.com/influxdata/influxdb1-client/v2"
)

const influxDbForeignQuotationTable = "foreignquotation"

// SaveForeignQuotationInflux stores a quotation which is not from DIA to an influx batch
func (datastore *DB) SaveForeignQuotationInflux(fq ForeignQuotation) error {
	fields := map[string]interface{}{
		"price":              fq.Price,
		"priceYesterday":     fq.PriceYesterday,
		"source":             fq.Source,
		"volumeYesterdayUSD": fq.VolumeYesterdayUSD,
	}
	tags := map[string]string{
		"symbol": fq.Symbol,
		"name":   fq.Name,
	}
	pt, err := clientInfluxdb.NewPoint(influxDbForeignQuotationTable, tags, fields, fq.Time)
	if err != nil {
		log.Errorln("NewOptionInflux:", err)
	} else {
		datastore.addPoint(pt)
	}
	err = datastore.WriteBatchInflux()
	if err != nil {
		log.Errorln("Write influx batch: ", err)
	}

	return err
}

// GetForeignQuotationInflux returns the last quotation of @symbol before @timestamp
func (datastore *DB) GetForeignQuotationInflux(symbol, source string, timestamp time.Time) (ForeignQuotation, error) {
	retval := ForeignQuotation{}

	unixtime := timestamp.UnixNano()
	q := fmt.Sprintf(
		"SELECT price,priceYesterday,volumeYesterdayUSD,\"name\" FROM %s WHERE source='%s' and \"symbol\"='%s' and time<%d order by time desc limit 1",
		influxDbForeignQuotationTable,
		source,
		symbol,
		unixtime,
	)
	res, err := queryInfluxDB(datastore.influxClient, q)
	if err != nil {
		fmt.Println("Error querying influx")
		return retval, err
	}

	if len(res) > 0 && len(res[0].Series) > 0 {
		layout := "2006-01-02T15:04:05Z"
		vals := res[0].Series[0].Values[0]

		retval.Time, err = time.Parse(layout, vals[0].(string))
		if err != nil {
			log.Error(err)
		}
		retval.Price, err = vals[1].(json.Number).Float64()
		if err != nil {
			log.Error(err)
		}
		retval.PriceYesterday, err = vals[2].(json.Number).Float64()
		if err != nil {
			log.Error(err)
		}
		retval.VolumeYesterdayUSD, err = vals[3].(json.Number).Float64()
		if err != nil {
			log.Error(err)
		}

		retval.Name = vals[4].(string)
		retval.Source = source
		retval.Symbol = symbol

		return retval, nil

	}
	return retval, err
}

// GetForeignPriceYesterday returns the average price of @symbol on @source from yesterday
func (datastore *DB) GetForeignPriceYesterday(symbol, source string) (float64, error) {

	// Get time range for yesterday in order to average the price
	now := time.Now()
	secondsFromYesterday := now.Hour()*60*60 + now.Minute()*60 + now.Second()
	timeFinal := int(now.Unix()) - secondsFromYesterday - 1
	timeInit := timeFinal - 24*60*60
	unixtimeFinal := strconv.Itoa(timeFinal) + "000000000"
	unixtimeInit := strconv.Itoa(timeInit) + "000000000"

	// Make corresponding influx query
	q := fmt.Sprintf("SELECT price FROM %s WHERE source='%s' and symbol='%s' and time>%s and time<%s", influxDbForeignQuotationTable, source, symbol, unixtimeInit, unixtimeFinal)
	res, err := queryInfluxDB(datastore.influxClient, q)
	if err != nil {
		fmt.Println("Error querying influx")
		return 0, err
	}

	// Simple average over all yesterday's prices
	var price float64
	errs := 0
	if len(res) > 0 && len(res[0].Series) > 0 && len(res[0].Series[0].Values) > 0 {
		numPrices := len(res[0].Series[0].Values)
		for i := range res[0].Series[0].Values {
			pricepoint, err := res[0].Series[0].Values[i][1].(json.Number).Float64()
			if err != nil {
				log.Error(err)
				errs++
			} else {
				price += pricepoint
			}

		}
		if numPrices > errs {
			return price / float64(numPrices-errs), nil
		}
	}
	return 0, errors.New("no data available from yesterday")
}

// GetForeignSymbolsInflux returns a list with all symbols available for quotation from @source.
func (datastore *DB) GetForeignSymbolsInflux(source string) (symbols []string, err error) {

	q := fmt.Sprintf("SELECT symbol,source FROM %s WHERE time>now()-7d and source='%s'", influxDbForeignQuotationTable, source)
	res, err := queryInfluxDB(datastore.influxClient, q)
	if err != nil {
		fmt.Println("Error querying influx")
		return
	}

	if len(res) > 0 && len(res[0].Series) > 0 {
		// make unique list of symbols
		vals := res[0].Series[0].Values
		set := make(map[string]struct{})
		symsUnique := []string{}
		for _, val := range vals {
			if _, ok := set[val[1].(string)]; !ok {
				symsUnique = append(symsUnique, val[1].(string))
				set[val[1].(string)] = struct{}{}
			}
		}

		// fill return slice
		for _, sym := range symsUnique {
			symbols = append(symbols, sym)
		}
	}
	return
}
