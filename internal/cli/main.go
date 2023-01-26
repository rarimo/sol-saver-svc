package cli

import (
	"context"

	"github.com/alecthomas/kingpin"
	"gitlab.com/distributed_lab/kit/kv"
	"gitlab.com/distributed_lab/logan/v3"
	rarimotypes "gitlab.com/rarimo/rarimo-core/x/rarimocore/types"
	"gitlab.com/rarimo/savers/saver-grpc-lib/voter"
	"gitlab.com/rarimo/savers/saver-grpc-lib/voter/verifiers"
	"gitlab.com/rarimo/savers/sol-saver-svc/internal/config"
	"gitlab.com/rarimo/savers/sol-saver-svc/internal/service/grpc"
	"gitlab.com/rarimo/savers/sol-saver-svc/internal/service/saver/catchup"
	"gitlab.com/rarimo/savers/sol-saver-svc/internal/service/saver/listener"
	voterservice "gitlab.com/rarimo/savers/sol-saver-svc/internal/service/voter"
)

func Run(args []string) bool {
	log := logan.New()

	defer func() {
		if rvr := recover(); rvr != nil {
			log.WithRecover(rvr).Error("app panicked")
		}
	}()

	cfg := config.New(kv.MustFromEnv())
	log = cfg.Log()

	app := kingpin.New("sol-saver-svc", "")

	runCmd := app.Command("run", "run command")

	voterCmd := runCmd.Command("voter", "run voter service")
	saverCmd := runCmd.Command("saver", "run saver service")
	saverCatchpuCmd := runCmd.Command("saver-catchup", "run saver service")

	serviceCmd := runCmd.Command("service", "run service") // you can insert custom help

	migrateCmd := app.Command("migrate", "migrate command")
	migrateUpCmd := migrateCmd.Command("up", "migrate db up")
	migrateDownCmd := migrateCmd.Command("down", "migrate db down")

	cmd, err := app.Parse(args[1:])
	if err != nil {
		log.WithError(err).Error("failed to parse arguments")
		return false
	}

	switch cmd {
	case voterCmd.FullCommand():
		verifier := verifiers.NewTransferVerifier(
			voterservice.NewTransferOperator(cfg),
			cfg.Log(),
		)

		v := voter.NewVoter(cfg.Log(), cfg.Broadcaster(), map[rarimotypes.OpType]voter.IVerifier{
			rarimotypes.OpType_TRANSFER: verifier,
		})

		voter.NewCatchupper(cfg.Cosmos(), v, cfg.Log()).Run()
		go voter.NewTransferSubscriber(v, cfg.Tendermint(), cfg.Cosmos(), cfg.Log()).Run()

		err = grpc.NewSaverService(cfg.Log(), cfg.Listener(), v).Run()
	case saverCmd.FullCommand():
		listener.NewService(cfg).Listen(context.TODO())
	case saverCatchpuCmd.FullCommand():
		err = catchup.NewService(cfg).Catchup(context.TODO())
	case serviceCmd.FullCommand():
		verifier := verifiers.NewTransferVerifier(
			voterservice.NewTransferOperator(cfg),
			cfg.Log(),
		)

		v := voter.NewVoter(cfg.Log(), cfg.Broadcaster(), map[rarimotypes.OpType]voter.IVerifier{
			rarimotypes.OpType_TRANSFER: verifier,
		})

		voter.NewCatchupper(cfg.Cosmos(), v, cfg.Log()).Run()
		go voter.NewTransferSubscriber(v, cfg.Tendermint(), cfg.Cosmos(), cfg.Log()).Run()
		go listener.NewService(cfg).Listen(context.TODO())

		err = grpc.NewSaverService(cfg.Log(), cfg.Listener(), v).Run()
	case migrateUpCmd.FullCommand():
		err = MigrateUp(cfg)
	case migrateDownCmd.FullCommand():
		err = MigrateDown(cfg)
	default:
		log.Errorf("unknown command %s", cmd)
		return false
	}
	if err != nil {
		log.WithError(err).Error("failed to exec cmd")
		return false
	}
	return true
}
