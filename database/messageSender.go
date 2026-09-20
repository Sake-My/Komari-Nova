package database

import (
	"github.com/Sake-My/Komari-Nova/database/dbcore"
	"github.com/Sake-My/Komari-Nova/database/models"
)

func GetMessageSenderConfigByName(name string) (*models.MessageSenderProvider, error) {
	db := dbcore.GetDBInstance()
	var config models.MessageSenderProvider
	if err := db.Where("name = ?", name).First(&config).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

func SaveMessageSenderConfig(config *models.MessageSenderProvider) error {
	db := dbcore.GetDBInstance()
	return db.Save(config).Error
}
