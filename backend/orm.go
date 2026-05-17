package main

type Preferences struct {
	ID        int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	PrefKey   string `gorm:"type:varchar(255);uniqueIndex;not null;column:pref_key" json:"prefKey"`
	PrefValue string `gorm:"type:text;column:pref_value" json:"prefValue"`
}

func tables() []interface{} {
	return []interface{}{
		&Preferences{},
	}
}
