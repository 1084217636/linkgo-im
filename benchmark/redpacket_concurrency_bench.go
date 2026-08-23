//go:build ignore

package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	corelogic "github.com/1084217636/linkgo-im/internal/logic"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := flag.String("dsn", "root:root@tcp(mysql:3306)/linkgo_im?charset=utf8mb4&parseTime=true", "benchmark MySQL DSN")
	claimers := flag.Int("claimers", 200, "number of concurrent unique claimers")
	count := flag.Int("count", 100, "red packet count")
	total := flag.Int64("total", 10000, "red packet total amount in fen")
	maxOpen := flag.Int("max-open-conns", 100, "client connection pool limit")
	flag.Parse()

	db, err := sql.Open("mysql", *dsn)
	must(err)
	defer db.Close()
	db.SetMaxOpenConns(*maxOpen)
	db.SetMaxIdleConns(40)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	must(db.PingContext(ctx))

	runID := fmt.Sprintf("rpbench-%d", time.Now().UnixNano())
	sender := runID + "-sender"
	wallet := corelogic.NewWalletService(db)
	_, err = wallet.MockRecharge(ctx, corelogic.MockRechargeParams{
		UserID: sender, RequestID: runID + "-recharge", Channel: "mock_wechat", Amount: *total,
	})
	must(err)
	packet, err := corelogic.NewRedPacketService(db).Create(ctx, corelogic.RedPacketCreateParams{
		SenderID: sender, ConversationID: "group:" + runID, ToType: "group",
		TotalAmount: *total, TotalCount: *count, Greeting: "concurrency benchmark",
	})
	must(err)

	start := make(chan struct{})
	var success, finished, failed atomic.Int64
	var claimedAmount atomic.Int64
	var wg sync.WaitGroup
	errorCounts := make(map[string]int)
	var errorMu sync.Mutex
	begin := time.Now()
	for i := 0; i < *claimers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			claim, claimErr := corelogic.NewRedPacketService(db).Claim(ctx, packet.ID, fmt.Sprintf("%s-user-%d", runID, i))
			switch {
			case claimErr == nil:
				success.Add(1)
				claimedAmount.Add(claim.Amount)
			case errors.Is(claimErr, corelogic.ErrRedPacketFinished):
				finished.Add(1)
			default:
				failed.Add(1)
				errorMu.Lock()
				errorCounts[claimErr.Error()]++
				errorMu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	detail, err := corelogic.NewRedPacketService(db).Detail(ctx, packet.ID, sender)
	must(err)
	invariantOK := success.Load() == int64(*count) &&
		claimedAmount.Load() == *total &&
		detail.Packet.RemainingCount == 0 &&
		detail.Packet.RemainingAmount == 0 &&
		len(detail.Claims) == *count
	fmt.Printf("redpacket_concurrency_result claimers=%d count=%d total=%d success=%d finished=%d failed=%d claimed_amount=%d remaining_count=%d remaining_amount=%d invariant_ok=%t elapsed=%s\n",
		*claimers, *count, *total, success.Load(), finished.Load(), failed.Load(), claimedAmount.Load(),
		detail.Packet.RemainingCount, detail.Packet.RemainingAmount, invariantOK, time.Since(begin))
	if len(errorCounts) > 0 {
		fmt.Printf("redpacket_concurrency_errors=%v\n", errorCounts)
	}
	if !invariantOK || failed.Load() != 0 {
		os.Exit(1)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
