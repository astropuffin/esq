// Copyright © 2017 Joseph Schneider <https://github.com/astropuffin>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"io"

	"encoding/json"

	"github.com/spf13/viper"
	"gopkg.in/olivere/elastic.v5"
	"time"

	"golang.org/x/sync/errgroup"
	//"gopkg.in/cheggaaa/pb.v1"
	"log"
)

func connection(q []string) {

	url := viper.GetString("url")
	options := []elastic.ClientOptionFunc{
		elastic.SetURL(url),
		elastic.SetSniff(false),
		elastic.SetHealthcheckTimeoutStartup(10 * time.Second),
		elastic.SetHealthcheckTimeout(2 * time.Second),
	}

	if viper.GetString("username") != "" {
		options = append(options,
			elastic.SetBasicAuth(viper.GetString("username"), viper.GetString("password")))
	}

	if verbose {
		options = append(options,
			elastic.SetTraceLog(log.New(os.Stderr, "ELASTIC ", log.LstdFlags)))
	}

	client, err := elastic.NewClient(options...)
	if err != nil {
		log.Fatalf("Could not connect Elasticsearch client to %s: %s.", url, err)
	}

	//result, _ := client.Search().
	//    Index(viper.GetString("index")).
	//    Query(elastic.NewQueryStringQuery(strings.Join(q, " "))).
	//    Sort(viper.GetString("timestamp"), false).
	//    From(0).
	//    Size(1).
	//    Do(context.Background())
	//fmt.Printf("Query took %d milliseconds\n", result.TookInMillis)

	// Count total and setup progress
	total, err := client.Count().
		Index(viper.GetString("index")).
		Query(elastic.NewQueryStringQuery(strings.Join(q, " "))).
		Do(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Printf("records: %d\n", total)
	//bar := pb.StartNew(int(total))

	hits := make(chan json.RawMessage)
	g, ctx := errgroup.WithContext(context.Background())
	scroll(client, q, hits, g, ctx)
	processRecords(hits, g, ctx)

	// Check whether any goroutines failed.
	if err := g.Wait(); err != nil {
		panic(err)
	}

	// Done.
	//bar.FinishPrint("Done")
}

func scroll(client *elastic.Client, q []string, hits chan json.RawMessage, g *errgroup.Group, ctx context.Context) error {
	g.Go(func() error {
		defer close(hits)
		// Initialize scroller. Just don't call Do yet.
		scroll := client.Scroll().
			Index(viper.GetString("index")).
			Query(elastic.NewQueryStringQuery(strings.Join(q, " "))).
			Sort(viper.GetString("timestamp"), false).
			Size(1000)

		for {
			results, err := scroll.Do(ctx)
			if err == io.EOF {
				return nil // all results retrieved
			}
			if err != nil {
				fmt.Println("something went wrong")
				return err // something went wrong
			}

			for _, hit := range results.Hits.Hits {
				hits <- *hit.Source
			}

			// Check if we need to terminate early
			select {
			default:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
	return nil
}

func processRecords(hits chan json.RawMessage, g *errgroup.Group, ctx context.Context) error {
	g.Go(func() error {
		for hit := range hits {
			var l map[string]interface{}
			err := json.Unmarshal(hit, &l)
			if err != nil {
				// Deserialization failed
			}
			fmt.Printf("%s", l["log"])

		}
		return nil
	})
	// Terminate early?
	select {
	default:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
