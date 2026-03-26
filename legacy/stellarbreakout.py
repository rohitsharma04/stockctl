import yfinance as yf
import pandas as pd
from datetime import datetime, timedelta
import time
from yfinance.exceptions import YFTzMissingError
import json


def check_volume_condition(data):
    # Calculate max volume of previous 5 weeks
    weekly_data = data.resample('W').sum()
    max_volume_last_5_weeks = weekly_data['Volume'].tail(5).max()
    
    # Calculate max weekly volume in 3 years, excluding last 3 weeks
    data_excluding_last_3_weeks = weekly_data.iloc[:-3]
    max_volume_3_years = data_excluding_last_3_weeks['Volume'].max()
    max_volume_3_years_half = max_volume_3_years * 0.5
    
    # Compare the two volumes
    return max_volume_last_5_weeks > max_volume_3_years_half

import pandas as pd

def check_close_condition(data):
    # Resample to weekly data, taking the last close of each week
    weekly_data = data.resample('W')['Close'].last()
    
    # Get the close from 2 weeks ago
    close_2_weeks_ago = weekly_data.iloc[-3]  # -3 because current week is included
    
    # Get the 52-week range starting from 3 weeks ago
    weeks_52_data = weekly_data.iloc[-55:-3]  # 55 weeks total, exclude last 3
    
    # Calculate the maximum close in this 52-week period
    max_close_52_weeks = weeks_52_data.max()
    
    # Calculate 61.8% of the maximum close
    threshold = max_close_52_weeks * 0.618
    
    # Check if the close from 2 weeks ago is greater than the threshold
    return close_2_weeks_ago > threshold


def check_conditions(data):
    # Resample to weekly data
    weekly_data = data.resample('W').agg({
        'Open': 'first',
        'Close': 'last',
        'Volume': 'sum'
    })
    
    # Calculate percentage changes
    weekly_data['Pct_Change'] = weekly_data['Close'].pct_change()
    
    # Get relevant data points
    two_weeks_ago = weekly_data.iloc[-3]
    one_week_ago = weekly_data.iloc[-2]
    
    # Check conditions
    condition1 = two_weeks_ago['Pct_Change'] > 0
    condition2 = one_week_ago['Pct_Change'] < 0
    condition3 = one_week_ago['Volume'] < two_weeks_ago['Volume']
    condition4 = two_weeks_ago['Open'] < one_week_ago['Close']
    
    # Return True if all conditions are met
    return all([condition1, condition2, condition3, condition4])

import pandas as pd

def is_bullish_with_heiken_ashi_confirmation(data):
    # Check if today's close is greater than 1 week ago's open
    weekly_data = data.resample('W').agg({'Open': 'first'})
    today_close = data['Close'].iloc[-1]
    one_week_ago_open = weekly_data['Open'].iloc[-2]
    condition1 = today_close > one_week_ago_open

    # Calculate Heikin-Ashi Close and Open
    ha_close = (data['Open'] + data['High'] + data['Low'] + data['Close']) / 4
    ha_open = (data['Open'].shift(1) + data['Close'].shift(1)) / 2
    
    # Check if today's Heikin-Ashi Close is greater than or equal to today's Heikin-Ashi Open
    condition2 = ha_close.iloc[-1] >= ha_open.iloc[-1]

    # Return True if both conditions are met
    return condition1 and condition2

# Example usage:
# result = is_bullish_with_heiken_ashi_confirmation(data)
# print(result)



def is_recent_volume_significant_compared_to_historical(stock_data):
    weekly_volume = stock_data.resample('W')['Volume'].sum()
    
    recent_volume_sma = weekly_volume.tail(5).mean()
    
    historical_period = weekly_volume.iloc[:-3]
    max_historical_volume = historical_period.max()
    
    volume_significance_threshold = 0.3
    is_volume_significant = recent_volume_sma >= volume_significance_threshold * max_historical_volume
    
    return is_volume_significant

# Example usage:
# result = is_recent_volume_significant_compared_to_historical(stock_data)
# print(result)





# Read the CSV file
all_tickers = pd.read_csv('us_stocks.csv')

# Set date range
end_date = datetime.now()
start_date = end_date - timedelta(days=1200)

# Initialize counter for executed tickers
executed_tickers_count = 0
total_tickers = len(all_tickers)

# Get today's date
current_date = datetime.now().strftime('%Y-%m-%d-%H:%M')

# Define the filename
filename = f'stellarbreakout-{current_date}.csv'

# Create a list to store the breakout caution tickers
breakout_caution_tickers = []


for _,item in all_tickers.iterrows():
    try:

        executed_tickers_count += 1
        ticker = item['Symbol'].replace('/', '-')
        # Create a Ticker object
        ticker_obj = yf.Ticker(ticker)
        data = ticker_obj.history(period='5y', interval='1d', auto_adjust=False) 


        # Check if data is empty (which can happen for delisted stocks)
        if data.empty:
            continue
        
        if (len(data) >= 756 and 
            data['Close'].iloc[-1].item() > 5 and 
            check_volume_condition(data).item() and
            check_close_condition(data).item() and
            check_conditions(data) and
            is_bullish_with_heiken_ashi_confirmation(data) and
            is_recent_volume_significant_compared_to_historical(data)):
            
            breakout_caution_tickers.append(ticker)
        time.sleep(0.1)
        
        
        print(f"Executed this: {ticker}, Executed Count: {executed_tickers_count}, Total: {total_tickers}")
        
    except YFTzMissingError:
        print(f"YFTzMissingError for {ticker}")
        continue
    except Exception as e:
        print(f"Error processing {ticker}: {str(e)}")
        continue

    # Write the tickers to the CSV file
with open(filename, 'w') as file:
    file.write('Ticker\n')  # Write the header
    for ticker in breakout_caution_tickers:
        file.write(f'{ticker}\n')
