import yfinance as yf
import pandas as pd
import numpy as np
from datetime import datetime, timedelta

def is_daily_high_above_bollinger(data, window=20, std_dev=2):
    sma = data['Close'].rolling(window=window).mean()
    std = data['Close'].rolling(window=window).std()
    upper_band = sma + (std * std_dev)
    return data['High'].iloc[-1] > upper_band.iloc[-1]

def is_volume_confirming_breakout(data):
    avg_volume = data['Volume'].rolling(window=10).mean()
    return data['Volume'].iloc[-1] > avg_volume.iloc[-1] * 1.5

def is_close_above_dynamic_sma(data, window=10):
    sma = data['Close'].rolling(window=window).mean()
    atr = calculate_atr(data, window)
    dynamic_sma = sma + (atr * 0.5)
    return data['Close'].iloc[-1] > dynamic_sma.iloc[-1]

def calculate_atr(data, window=14):
    high_low = data['High'] - data['Low']
    high_close = np.abs(data['High'] - data['Close'].shift())
    low_close = np.abs(data['Low'] - data['Close'].shift())
    ranges = pd.concat([high_low, high_close, low_close], axis=1)
    true_range = np.max(ranges, axis=1)
    return true_range.rolling(window=window).mean()

def calculate_relative_strength(data, benchmark_data):
    stock_returns = data['Close'].pct_change()
    benchmark_returns = benchmark_data['Close'].pct_change()
    return (stock_returns / benchmark_returns).rolling(window=20).mean().iloc[-1]

def check_breakout_conditions(data, benchmark_data):
    return (
        len(data) >= 252 and  # At least 1 year of data
        data['Close'].iloc[-1] > 5 and
        ((data['Close'].iloc[-1] - data['Close'].iloc[-22]) / data['Close'].iloc[-22]) > 0.1 and  # 10% rise in last month
        is_daily_high_above_bollinger(data) and
        is_volume_confirming_breakout(data) and
        is_close_above_dynamic_sma(data) and
        calculate_relative_strength(data, benchmark_data) > 1.05
    )

def main(backtest_date):
    all_tickers = pd.read_csv('us_stocks.csv')
    end_date = datetime.strptime(backtest_date, '%Y-%m-%d')
    start_date = end_date - timedelta(days=365)  # 1 year of data
    filename = f'us_breakout_opportunities_{backtest_date}.csv'
    breakout_opportunities = []

    benchmark = yf.Ticker('^GSPC')
    benchmark_data = benchmark.history(start=start_date, end=end_date)

    total_tickers = len(all_tickers)
    processed_count = 0

    for _, item in all_tickers.iterrows():
        try:
            ticker = item['Symbol'].replace('/', '-')
            ticker_obj = yf.Ticker(ticker)
            data = ticker_obj.history(start=start_date, end=end_date)

            if data.empty:
                continue

            if check_breakout_conditions(data, benchmark_data):
                breakout_opportunities.append(ticker)

            processed_count += 1
            print(f"Processed: {ticker} ({processed_count}/{total_tickers})")

        except Exception as e:
            print(f"Error processing {ticker}: {str(e)}")
            continue

    pd.DataFrame({'Ticker': breakout_opportunities}).to_csv(filename, index=False)
    print(f"Results written to {filename}")
    print(f"Total stocks processed: {processed_count}")
    print(f"Total stocks meeting breakout conditions: {len(breakout_opportunities)}")

if __name__ == "__main__":
    backtest_date = input("Enter the backtest date (YYYY-MM-DD): ")
    main(backtest_date)
