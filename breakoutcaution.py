import yfinance as yf
import pandas as pd
from datetime import datetime, timedelta
import time
from yfinance.exceptions import YFTzMissingError
import json

def is_weekly_high_above_bollinger(data):
    # Resample to weekly data
    weekly_data = data.resample('W').agg({'High': 'max', 'Close': 'last'})
    
    # Calculate the rolling mean and standard deviation for Bollinger Bands
    window = 20  # Typical window size for Bollinger Bands
    sma = weekly_data['Close'].rolling(window=window).mean()
    std = weekly_data['Close'].rolling(window=window).std()
    upper_band = sma + (std * 2)
    
    # Get the latest weekly high and upper Bollinger Band
    latest_weekly_high = weekly_data['High'].iloc[-1]
    latest_upper_band = upper_band.iloc[-1]
    
    # Compare the values
    return latest_weekly_high > latest_upper_band

def is_last_week_close_below_156_week_high(data):
    # Resample to weekly data
    weekly_data = data.resample('W').agg({'Close': 'last'})
    
    # Get the last 157 weeks of data (156 weeks + current week)
    last_157_weeks = weekly_data['Close'].tail(157)
    
    # Calculate the max close of the previous 156 weeks
    max_close_156_weeks = last_157_weeks.iloc[:-2].max()
    
    # Get the last week's close
    last_week_close = last_157_weeks.iloc[-2]
    
    # Compare the values
    return last_week_close < max_close_156_weeks

def is_close_above_sma_20(data):
    # Calculate the 20-day Simple Moving Average (SMA)
    sma_20 = data['Close'].rolling(window=20).mean()
    
    # Get the latest closing price
    latest_close = data['Close'].iloc[-1]
    
    # Get the latest 20-day SMA
    latest_sma_20 = sma_20.iloc[-1]
    
    # Check if the latest closing price is greater than the 20-day SMA
    return latest_close > latest_sma_20

def is_last_week_close_less_than_open(data):
    # Resample to weekly data, aggregating Open and Close prices
    weekly_data = data.resample('W').agg({'Open': 'first', 'Close': 'last'})
    
    # Ensure there are enough weeks of data
    if len(weekly_data) < 1:
        raise ValueError("Not enough weekly data to perform the check.")
    
    # Get the last week's open and close
    last_week_open = weekly_data['Open'].iloc[-2]
    last_week_close = weekly_data['Close'].iloc[-2]
    
    # Check if the last week's close is less than the last week's open
    return last_week_close < last_week_open

def is_weekly_close_above_sma_20(data):
    # Resample to weekly data
    weekly_data = data.resample('W').agg({'Close': 'last'})
    
    # Calculate the 20-week Simple Moving Average (SMA)
    weekly_data['SMA_20'] = weekly_data['Close'].rolling(window=20).mean()
    
    # Compare the latest weekly close with the latest 20-week SMA
    latest_weekly_close = weekly_data['Close'].iloc[-1]
    latest_sma_20 = weekly_data['SMA_20'].iloc[-1]
    
    return latest_weekly_close > latest_sma_20

def is_above_monthly_middle_bollinger(data):
    # Resample daily data to monthly
    monthly_data = data.resample('M').agg({'Close': 'last'})
    
    # Calculate the 20-month SMA (middle Bollinger Band)
    monthly_data['SMA20'] = monthly_data['Close'].rolling(window=20).mean()
    
    # Get the last three months of data
    last_three_months = monthly_data.tail(3)
    
    # Check if the close is greater than the SMA20 for all three months
    return all(last_three_months['Close'] > last_three_months['SMA20'])



def check_weekly_and_daily_conditions(data):
    # Resample to weekly data
    weekly_data = data.resample('W').agg({'Close': 'last'})
    
    # Calculate the maximum close of the last 156 weeks
    max_close_156_weeks = weekly_data['Close'].tail(156).max()
    
    # Get the latest weekly close
    latest_weekly_close = weekly_data['Close'].iloc[-1]
    
    # Get the latest daily close
    latest_daily_close = data['Close'].iloc[-1]
    
    # Check if weekly close is greater than or equal to max of 156 week close
    condition1 = latest_weekly_close >= max_close_156_weeks
    
    # Check if daily close is within 5% of 156 week max close
    condition2 = latest_daily_close >= 0.95 * max_close_156_weeks
    
    # Return True if either condition is met
    return condition1 or condition2

def stock_risen_30_percent(data):
    
    # Get the closing price 6 months ago and the latest closing price
    six_months_ago = data['Close'].iloc[-126]  # Assuming 21 trading days per month
    latest_price = data['Close'].iloc[-1]
    
    # Calculate the percentage change
    percentage_change = ((latest_price - six_months_ago) / six_months_ago) * 100
    
    # Check if the percentage change is greater than 30%
    return percentage_change > 30





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
filename = f'breakoutcaution-{current_date}.csv'

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
            stock_risen_30_percent(data) and
            is_weekly_high_above_bollinger(data).item() and 
            is_last_week_close_below_156_week_high(data).item() and 
            is_close_above_sma_20(data).item() and 
            is_last_week_close_less_than_open(data).item() and 
            is_weekly_close_above_sma_20(data).item() and 
            is_above_monthly_middle_bollinger(data) and 
            check_weekly_and_daily_conditions(data)):
        
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
