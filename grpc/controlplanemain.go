package main
import (
	"context"
	"log"
	"os"
	"github.com/urfave/cli/v3"
)
func main () {
	cmd := &cli.Command{
		Usage: "control plane for 4a.si/razpravljalnica",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name: "bind",
				Value: "[::]:9870",
				Usage: "url to bind controlplane grpc server to",
			},
			&cli.StringFlag{
				Name: "s2skey",
				Value: "skrivnost",
				Usage: "preshared key za avtentikacijo s2s komunikacije",
			},
		},
		Action: func (ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() > 0 {
				return cli.Exit("Program ne sprejema argumentov. Poženite s stikalom --help za navodila.", 1)
			}
			return ControlPlaneServer(cmd.String("bind"), cmd.String("s2skey"))
		},
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
