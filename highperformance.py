import yfinance as yf
import pandas as pd
from datetime import datetime, timedelta
import time
from yfinance.exceptions import YFTzMissingError
import json


def is_200_sma_below_50_sma(data):
    sma_200 = data['Close'].rolling(window=200).mean()
    sma_50 = data['Close'].rolling(window=50).mean()
    return sma_200.iloc[-1] < sma_50.iloc[-1]


def is_close_above_sma_50(data):
    # Calculate the 50-day Simple Moving Average (SMA)
    sma_50 = data['Close'].rolling(window=50).mean()
    
    # Get the latest closing price
    latest_close = data['Close'].iloc[-1]
    
    # Get the latest 50-day SMA
    latest_sma_50 = sma_50.iloc[-1]
    
    # Check if the latest closing price is greater than the 50-day SMA
    return latest_close > latest_sma_50


def is_close_above_twice_min_252(data):
    # Calculate the minimum close price over the last 252 days
    min_close_252 = data['Close'].tail(252).min()
    
    # Get today's close price
    today_close = data['Close'].iloc[-1]
    
    # Check if today's close is greater than 2 times the minimum close
    return today_close > 2 * min_close_252


def check_consistent_max_close(data):
    # Calculate max close for different periods
    max_126 = data['Close'].tail(126).max()
    max_252 = data['Close'].tail(252).max()
    max_378 = data['Close'].tail(378).max()
    
    # Check if max_126 equals max_252 for current period
    current_condition = max_126 == max_252
    
    # Go back 126 days and check again
    data_126_ago = data.iloc[:-126]
    max_126_126ago = data_126_ago['Close'].tail(126).max()
    max_252_126ago = data_126_ago['Close'].tail(252).max()
    condition_126_ago = max_126_126ago == max_252_126ago
    
    # Go back 252 days and check again
    data_252_ago = data.iloc[:-252]
    max_126_252ago = data_252_ago['Close'].tail(126).max()
    max_252_252ago = data_252_ago['Close'].tail(252).max()
    condition_252_ago = max_126_252ago == max_252_252ago
    
    # Go back 378 days and check again
    data_378_ago = data.iloc[:-378]
    max_126_378ago = data_378_ago['Close'].tail(126).max()
    max_252_378ago = data_378_ago['Close'].tail(252).max()
    condition_378_ago = max_126_378ago == max_252_378ago
    
    # Check if all conditions are met
    return all([current_condition, condition_126_ago, condition_252_ago, condition_378_ago])


def has_sma_200_increased_90_days(data):
    # Calculate the 200-day SMA
    data['SMA_200'] = data['Close'].rolling(window=200).mean()
    
    # Get the last 90 days of SMA_200
    sma_200_last_90 = data['SMA_200'].tail(90)
    
    # Check if the SMA_200 has only increased over the past 90 days
    return sma_200_last_90.is_monotonic_increasing


def is_close_above_75_percent_max(data):
    max_close_252 = data['Close'].tail(252).max()
    today_close = data['Close'].iloc[-1]
    return today_close >= 0.75 * max_close_252

import pandas as pd

def is_close_above_70_percent_max_126(data):

    for i in range(-252, 0):
        max_close_126 = data['Close'].iloc[i-126:i].max()
        daily_close = data['Close'].iloc[i]
        if daily_close < 0.7 * max_close_126:
            return False

    return True



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
filename = f'highperformance-{current_date}.csv'

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
            is_200_sma_below_50_sma(data).item() and
            is_close_above_sma_50(data).item() and
            is_close_above_twice_min_252(data).item() and
            check_consistent_max_close(data) and
            has_sma_200_increased_90_days(data) and
            is_close_above_75_percent_max(data).item() and
            is_close_above_70_percent_max_126(data)):
            
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
