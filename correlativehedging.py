import yfinance as yf
import pandas as pd
import numpy as np
import matplotlib.pyplot as plt
from scipy import stats
from datetime import datetime, timedelta
from sklearn.linear_model import LinearRegression

# List of F&O stocks (abbreviated for brevity)
fo_stocks = [
    'RELIANCE.NS', 'TCS.NS', 'HDFCBANK.NS', 'INFY.NS', 'ICICIBANK.NS',
    'HINDUNILVR.NS', 'ITC.NS', 'SBIN.NS', 'BHARTIARTL.NS', 'KOTAKBANK.NS'
]

# Download historical data for F&O stocks
data = yf.download(fo_stocks, start='2020-04-30', end='2025-01-29')['Adj Close']

# Calculate returns
returns = data.pct_change().dropna()

# Calculate correlation matrix
corr_matrix = returns.corr()

# Function to find pairs with strong correlation
def find_correlated_pairs(corr_matrix, threshold=0.7):
    pairs = []
    for i in range(len(corr_matrix.columns)):
        for j in range(i+1, len(corr_matrix.columns)):
            if corr_matrix.iloc[i, j] > threshold:
                pairs.append((corr_matrix.columns[i], corr_matrix.columns[j], corr_matrix.iloc[i, j]))
    return pairs

# Find correlated pairs
correlated_pairs = find_correlated_pairs(corr_matrix)

# Function to calculate hedge ratio using OLS regression
def calculate_hedge_ratio(data, stock1, stock2):
    X = data[stock1].values.reshape(-1, 1)
    y = data[stock2].values
    model = LinearRegression().fit(X, y)
    return model.coef_[0]

# Function to calculate normal price ratio and simulate trading
def pairs_trading_simulation(data, stock1, stock2, window=50, z_threshold=2, initial_capital=100000):
    pair_data = data[[stock1, stock2]].dropna()
    
    # Calculate hedge ratio
    hedge_ratio = calculate_hedge_ratio(pair_data, stock1, stock2)
    
    # Calculate spread using hedge ratio
    spread = pair_data[stock1] - hedge_ratio * pair_data[stock2]
    
    ma = spread.rolling(window=window).mean()
    std = spread.rolling(window=window).std()
    z_score = (spread - ma) / std
    
    signals = pd.DataFrame(index=pair_data.index)
    signals['z_score'] = z_score
    signals['long_entry'] = (signals['z_score'] < -z_threshold).astype(int)
    signals['short_entry'] = (signals['z_score'] > z_threshold).astype(int)
    signals['exit'] = ((signals['z_score'] > -0.5) & (signals['z_score'] < 0.5)).astype(int)
    
    position = 0
    entry_price1 = 0
    entry_price2 = 0
    trades = []
    capital = initial_capital
    
    for i in range(1, len(signals)):
        if position == 0:
            if signals['long_entry'].iloc[i] == 1:
                position = 1
                entry_price1 = pair_data[stock1].iloc[i]
                entry_price2 = pair_data[stock2].iloc[i]
                long_stock = stock1
                short_stock = stock2
            elif signals['short_entry'].iloc[i] == 1:
                position = -1
                entry_price1 = pair_data[stock1].iloc[i]
                entry_price2 = pair_data[stock2].iloc[i]
                long_stock = stock2
                short_stock = stock1
        elif position != 0 and signals['exit'].iloc[i] == 1:
            exit_price1 = pair_data[stock1].iloc[i]
            exit_price2 = pair_data[stock2].iloc[i]
            
            if position == 1:
                profit = (exit_price1 - entry_price1) - hedge_ratio * (exit_price2 - entry_price2)
            else:
                profit = (entry_price1 - exit_price1) - hedge_ratio * (entry_price2 - exit_price2)
            
            if pair_data.index[i-1].replace(tzinfo=None) > datetime.now() - timedelta(days=365):
                trades.append({
                    'entry_date': pair_data.index[i-1],
                    'exit_date': pair_data.index[i],
                    'profit': profit,
                    'position': position,
                    'long_stock': long_stock,
                    'short_stock': short_stock,
                    'amount': initial_capital / 2,
                    'hedge_ratio': hedge_ratio
                })
            
            capital += profit
            position = 0
    
    return trades, capital, hedge_ratio

# Simulate trading for each correlated pair
for pair in correlated_pairs[:5]:  # Limiting to top 5 pairs for brevity
    stock1, stock2, _ = pair
    trades, final_capital, hedge_ratio = pairs_trading_simulation(data, stock1, stock2, initial_capital=100000)
    
    print(f"\nPair Trading Results for {stock1} - {stock2}:")
    print(f"Hedge Ratio: {hedge_ratio:.4f}")
    print(f"Number of trades: {len(trades)}")
    print(f"Final capital: ₹{final_capital:.2f}")
    print(f"Total profit: ₹{final_capital - 100000:.2f}")
    
    if trades:
        profitable_trades = sum(1 for trade in trades if trade['profit'] > 0)
        print(f"Profitable trades: {profitable_trades} ({profitable_trades/len(trades)*100:.2f}%)")
        
        plt.figure(figsize=(12, 6))
        for trade in trades:
            color = 'g' if trade['profit'] > 0 else 'r'
            plt.plot([trade['entry_date'], trade['exit_date']], [trade['profit'], trade['profit']], color=color, marker='o')
            plt.annotate(f"Long: {trade['long_stock']}\nShort: {trade['short_stock']}\n"
                         f"Amount: ₹{trade['amount']:.2f}\n"
                         f"Entry: {trade['entry_date'].strftime('%Y-%m-%d')}\n"
                         f"Exit: {trade['exit_date'].strftime('%Y-%m-%d')}\n"
                         f"Profit: ₹{trade['profit']:.2f}", 
                         (trade['exit_date'], trade['profit']), 
                         xytext=(10, 0), textcoords='offset points', ha='left', va='center',
                         bbox=dict(boxstyle='round,pad=0.5', fc='yellow', alpha=0.5),
                         arrowprops=dict(arrowstyle='->', connectionstyle='arc3,rad=0'))
        
        plt.title(f'Trade Profits for {stock1} - {stock2}')
        plt.xlabel('Exit Date')
        plt.ylabel('Profit (₹)')
        plt.axhline(y=0, color='k', linestyle='-')
        plt.xticks(rotation=45)
        plt.tight_layout()
        plt.show()
    else:
        print("No trades executed for this pair.")
