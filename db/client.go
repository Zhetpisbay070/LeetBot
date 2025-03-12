package db

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

func Create(cfg Config) (*mongo.Client, error) {
	mongoClient, err := mongo.Connect(context.Background(), options.Client().ApplyURI(cfg.Url))

	if err != nil {
		log.Fatalf("connection error :%v", err)
		return nil, err
	}

	err = mongoClient.Ping(context.Background(), readpref.Primary())
	if err != nil {
		log.Fatalf("ping mongodb error :%v", err)
		return nil, err
	}

	return mongoClient, nil
}
